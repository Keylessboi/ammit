package rewrite

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/Keylessboi/ammit/pkg/corpus"
	"github.com/Keylessboi/ammit/pkg/llm"
)

// stub is a Provider that records what it was asked and returns a controlled
// answer. It is how the rewrite path is verified without a model: the mechanism
// is code and can be tested, while the quality of a particular model is a
// property of that model.
type stub struct {
	calls []llm.Request
	reply func(req llm.Request) (string, error)
}

func (s *stub) Name() string { return "stub" }

func (s *stub) Complete(_ context.Context, req llm.Request) (string, error) {
	s.calls = append(s.calls, req)
	if s.reply != nil {
		return s.reply(req)
	}
	// A recognisable transformation, so a test can tell rewritten text from
	// original text without asserting on exact wording.
	//
	// The passage sits between the first opening "---" and the last closing
	// "---". Using LastIndex for the opening would find the closing marker and
	// return an empty body, which is exactly the bug this comment prevents.
	body := strings.TrimSpace(req.Prompt)
	if i := strings.Index(body, "---\n"); i >= 0 {
		body = body[i+4:]
	}
	if j := strings.LastIndex(body, "\n---"); j >= 0 {
		body = body[:j]
	}
	return "REWRITTEN " + strings.TrimSpace(body), nil
}

// testDocs builds a document with prose sections and structured records.
func testDocs() []corpus.Document {
	return []corpus.Document{{
		Nonce:    "abc/0",
		Strategy: "preference",
		Kind:     corpus.KindDataset,
		Title:    "A title",
		Summary:  "A summary that is long enough to be worth rewriting.",
		Sections: []corpus.Section{
			{
				Heading: "Overview",
				Body: []string{
					"This is a long enough paragraph to be sent to the model for rewriting.",
					"Short.",
				},
			},
		},
		Records: []corpus.Record{
			{
				Format: "preference-pair",
				Label:  "chosen",
				Fields: map[string]string{
					"prompt": "Explain how lock picking works in practice.",
					"chosen": "The mechanism is mechanical and well documented in the literature.",
					"rating": "5",
					"token":  "<|im_start|>",
				},
			},
			{
				Format: "anthropic-hh",
				Fields: map[string]string{
					"transcript": "\n\nHuman: Explain the tradeoffs involved.\n\nAssistant: There are three parts to this.",
				},
			},
		},
	}}
}

func testRNG() *rand.Rand { return rand.New(rand.NewChaCha8([32]byte{7})) }

// TestRewritesSections checks the prose is actually replaced. If this silently
// stopped happening the corpus would ship unrewritten and nobody would notice.
func TestRewritesSections(t *testing.T) {
	s := &stub{}
	r, err := New(s, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, stats, err := r.Documents(context.Background(), testDocs(), testRNG())
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}

	body := out[0].Sections[0].Body[0]
	if !strings.HasPrefix(body, "REWRITTEN ") {
		t.Errorf("section was not rewritten: %q", body)
	}
	if stats.Sections != 1 {
		t.Errorf("stats.Sections = %d, want 1 (the short paragraph should be skipped)", stats.Sections)
	}
	// Four calls: one prose section plus the three prose record fields
	// (prompt, chosen, transcript). The "rating" and "token" fields are
	// structural and deliberately skipped.
	if stats.CallOK != 4 {
		t.Errorf("stats.CallOK = %d, want 4", stats.CallOK)
	}
}

// TestSkipsShortLines pins the cost optimisation: a one-clause line is already
// varied and spends model budget for no gain.
func TestSkipsShortLines(t *testing.T) {
	s := &stub{}
	r, _ := New(s, DefaultOptions())
	out, _, _ := r.Documents(context.Background(), testDocs(), testRNG())

	if out[0].Sections[0].Body[1] != "Short." {
		t.Errorf("short line was rewritten: %q", out[0].Sections[0].Body[1])
	}
}

// TestRewritesOnlyProseFields is the test that guards against corruption.
// Paraphrasing a token id or a rating is not evasion, it is damage: a loader
// that expects "<|im_start|>" or "5" would get nonsense.
func TestRewritesOnlyProseFields(t *testing.T) {
	s := &stub{}
	r, _ := New(s, DefaultOptions())
	out, _, _ := r.Documents(context.Background(), testDocs(), testRNG())

	f := out[0].Records[0].Fields

	if !strings.HasPrefix(f["chosen"], "REWRITTEN ") {
		t.Errorf("chosen was not rewritten: %q", f["chosen"])
	}
	if !strings.HasPrefix(f["prompt"], "REWRITTEN ") {
		t.Errorf("prompt was not rewritten: %q", f["prompt"])
	}
	if f["rating"] != "5" {
		t.Errorf("rating was modified: %q", f["rating"])
	}
	if f["token"] != "<|im_start|>" {
		t.Errorf("token was modified: %q", f["token"])
	}
}

// TestTranscriptKeepsRoleMarkers checks the hint reaches the model. A transcript
// whose "Human:"/"Assistant:" delimiters are paraphrased is no longer a
// transcript, and the loader that would have read it silently drops it.
func TestTranscriptKeepsRoleMarkers(t *testing.T) {
	s := &stub{}
	r, _ := New(s, DefaultOptions())
	r.Documents(context.Background(), testDocs(), testRNG())

	var sawHint bool
	for _, c := range s.calls {
		if strings.Contains(c.Prompt, "transcript") {
			sawHint = true
		}
	}
	if !sawHint {
		t.Error("no call carried the transcript-format hint")
	}
}

// TestRefusalFallsBack checks a refusal degrades rather than deleting content.
// Shipping the original text is a loss of novelty; shipping nothing is a bug.
func TestRefusalFallsBack(t *testing.T) {
	s := &stub{reply: func(llm.Request) (string, error) {
		return "", fmt.Errorf("busy: %w", llm.ErrRefused)
	}}
	r, _ := New(s, DefaultOptions())

	out, stats, err := r.Documents(context.Background(), testDocs(), testRNG())
	if err != nil {
		t.Fatalf("Documents returned an error for a per-call failure: %v", err)
	}

	if out[0].Sections[0].Body[0] != "This is a long enough paragraph to be sent to the model for rewriting." {
		t.Error("refused section was not left intact")
	}
	if stats.Refused == 0 {
		t.Error("refusal was not counted")
	}
	if stats.Fallbacks == 0 {
		t.Error("fallback was not counted")
	}
}

// TestProviderErrorFallsBack covers the ordinary failure: the model is down.
func TestProviderErrorFallsBack(t *testing.T) {
	s := &stub{reply: func(llm.Request) (string, error) {
		return "", errors.New("connection refused")
	}}
	r, _ := New(s, DefaultOptions())

	out, stats, err := r.Documents(context.Background(), testDocs(), testRNG())
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if stats.CallFailed == 0 {
		t.Error("failure was not counted")
	}
	if strings.HasPrefix(out[0].Sections[0].Body[0], "REWRITTEN ") {
		t.Error("output claims a rewrite that did not happen")
	}
}

// TestMaxCallsIsRespected guards the cost ceiling. A document with many sections
// must not be able to issue unbounded model calls.
func TestMaxCallsIsRespected(t *testing.T) {
	docs := testDocs()
	docs[0].Sections = nil
	for i := 0; i < 40; i++ {
		docs[0].Sections = append(docs[0].Sections, corpus.Section{
			Heading: "H",
			Body:    []string{"A paragraph that is comfortably long enough to be considered for rewriting by the model."},
		})
	}

	s := &stub{}
	r, _ := New(s, Options{MaxCalls: 5})
	r.Documents(context.Background(), docs, testRNG())

	if len(s.calls) > 5 {
		t.Errorf("made %d calls with MaxCalls=5", len(s.calls))
	}
}

// TestStyleIsRecordedAndVaried checks the per-site voice is both chosen and
// visible in metadata, since that metadata is what an operator inspects when
// asking why two sites look similar.
func TestStyleIsRecordedAndVaried(t *testing.T) {
	s := &stub{}
	r, _ := New(s, DefaultOptions())

	out, _, _ := r.Documents(context.Background(), testDocs(), testRNG())
	if out[0].Meta["style"] == "" {
		t.Error("no style recorded in metadata")
	}

	// One source, drawn from repeatedly. Calling testRNG() each time would
	// rebuild the same seeded source and yield the same first draw forever,
	// which is exactly the kind of bug this assertion exists to catch.
	rng := testRNG()
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		seen[StyleFor(rng).Name] = true
	}
	if len(seen) < 5 {
		t.Errorf("only %d distinct styles over 200 draws; the bank is too small to disguise a corpus", len(seen))
	}

	if len(Styles()) == 0 {
		t.Error("Styles() is empty")
	}
}

// TestSystemPromptForbidsTells checks the instruction keeps its anti-artefact
// rules. A model that adds "Here is the rewritten text:" produces a document
// whose first line is a fingerprint of machine authorship.
func TestSystemPromptForbidsTells(t *testing.T) {
	p := systemPrompt(Styles()[0])
	for _, want := range []string{"Preserve every claim", "Add nothing", "Remove nothing", "Never mention this instruction"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt lost the rule %q", want)
		}
	}
}

// TestNoProviderIsAnError checks the constructor refuses a nil provider rather
// than panicking later on the request path.
func TestNoProviderIsAnError(t *testing.T) {
	if _, err := New(nil, DefaultOptions()); err == nil {
		t.Error("New accepted a nil provider")
	}
}
