package scramble

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

func rng(seed byte) *rand.Rand {
	return rand.New(rand.NewChaCha8([32]byte{seed}))
}

// TestSubstitutesStockPhrases is the core claim. If this fails the package does
// nothing and the corpus ships with the repetition it was written to remove.
func TestSubstitutesStockPhrases(t *testing.T) {
	s := New(DefaultOptions())

	cases := []string{
		"a held-out probe set",
		"a held out probe set",
		"and why that is easy to overlook",
		"are not independent",
		"put it plainly",
		"with some regularity",
	}
	for _, in := range cases {
		out := s.Scramble(in, rng(1))
		if out == in {
			t.Errorf("no substitution for %q", in)
		}
	}
}

// TestDeterministic pins reproducibility. The same source must give the same
// result, or a site's output changes on every request and stops being a
// coordinated corpus.
func TestDeterministic(t *testing.T) {
	s := New(DefaultOptions())
	const in = "a held-out probe set and why that is easy to overlook"

	a := s.Scramble(in, rng(7))
	b := s.Scramble(in, rng(7))
	if a != b {
		t.Fatalf("same source gave different results:\n%q\n%q", a, b)
	}
}

// TestSitesDiffer checks the other half: different sources pick different
// wording, so two sites do not emit the same phrases.
func TestSitesDiffer(t *testing.T) {
	s := New(DefaultOptions())
	const in = "a held-out probe set and why that is easy to overlook"

	seen := map[string]bool{}
	for i := byte(0); i < 32; i++ {
		seen[s.Scramble(in, rng(i))] = true
	}
	if len(seen) < 3 {
		t.Errorf("only %d distinct renderings over 32 sources", len(seen))
	}
}

// TestInversionKeepsCapital checks a bug that shipped once: moving a leading
// adverbial to the end stripped the capital from the sentence.
func TestInversionKeepsCapital(t *testing.T) {
	out := invertAdverbials("In practice, the numbers disagree.")

	first := out[0]
	if first < 'A' || first > 'Z' {
		t.Errorf("sentence starts in lower case: %q", out)
	}
	if !strings.Contains(out, "in practice") {
		t.Errorf("adverbial was lost: %q", out)
	}
}

// TestInversionSkipsDeterminers checks the guard against turning "The numbers
// disagree" into "numbers disagree, the".
func TestInversionSkipsDeterminers(t *testing.T) {
	const in = "The numbers disagree."
	if got := invertAdverbials(in); got != in {
		t.Errorf("determiner was moved: %q", got)
	}
}

// TestJSONFieldsKeepTheirShape checks structured records survive. Corrupting a
// message array would break the loader that reads it, which is worse than not
// scrambling at all.
func TestJSONFieldsKeepTheirShape(t *testing.T) {
	s := New(DefaultOptions())
	in := `[{"role":"user","content":"Explain a held-out probe set for me."},{"role":"assistant","content":"Sure, a held-out probe set is used."}]`

	out := s.scrambleField(in, rng(3))

	var arr []map[string]string
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(arr) != 2 {
		t.Fatalf("message count changed: %d", len(arr))
	}
	if arr[0]["role"] != "user" || arr[1]["role"] != "assistant" {
		t.Errorf("roles were modified: %v", arr)
	}
	if !strings.Contains(arr[0]["content"], "Explain") {
		t.Errorf("content was destroyed: %q", arr[0]["content"])
	}
}

// TestUnparseableJSONIsLeftAlone checks the safe path: an unrecognised shape is
// returned unchanged rather than mangled.
func TestUnparseableJSONIsLeftAlone(t *testing.T) {
	s := New(DefaultOptions())
	const in = `[not json at all`
	if got := s.scrambleField(in, rng(4)); got != in {
		t.Errorf("unparseable input was modified: %q", got)
	}
}

// TestExpandsTemplates checks the mechanism that turns a small written bank into
// a large effective one. Without it, four alternatives across forty documents
// produce ten-way collisions, which is the repetition the package exists to
// remove.
func TestExpandsTemplates(t *testing.T) {
	got := expandOne("{a|b} x {c|d|e}")
	if len(got) != 6 {
		t.Fatalf("expected 6 combinations, got %d: %v", len(got), got)
	}
	want := map[string]bool{"a x c": true, "a x d": true, "a x e": true, "b x c": true, "b x d": true, "b x e": true}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected combination %q", g)
		}
	}
}

// TestEveryBankEntryIsExpandable guards against a malformed entry producing an
// empty alternative, which would delete text rather than replace it.
func TestEveryBankEntryIsExpandable(t *testing.T) {
	for k, opts := range phraseBank {
		ex := expandTemplates(opts)
		if len(ex) == 0 {
			t.Errorf("phrase %q expands to nothing", k)
		}
		for _, e := range ex {
			if strings.TrimSpace(e) == "" {
				t.Errorf("phrase %q has an empty alternative", k)
			}
		}
	}
	for k, opts := range wordBank {
		if len(expandTemplates(opts)) == 0 {
			t.Errorf("word %q expands to nothing", k)
		}
	}
}

// TestDocumentsScramblesEveryReadableSurface checks the bug that shipped once:
// the summary was left untouched, so a stock phrase kept reaching a majority of
// documents even though the body had already been fixed.
func TestDocumentsScramblesEveryReadableSurface(t *testing.T) {
	s := New(DefaultOptions())
	docs := []corpus.Document{{
		Title:    "Notes on a held-out probe set",
		Summary:  "And why that is easy to overlook, with some regularity.",
		Sections: []corpus.Section{{Heading: "H", Body: []string{"In practice, are not independent."}}},
	}}

	out := s.Documents(docs, rng(5))

	if out[0].Summary == docs[0].Summary {
		t.Error("summary was not scrambled")
	}
	if out[0].Title == docs[0].Title {
		t.Error("title was not scrambled")
	}
	if out[0].Sections[0].Body[0] == docs[0].Sections[0].Body[0] {
		t.Error("section body was not scrambled")
	}
	if out[0].Meta["scrambled"] != "true" {
		t.Error("scrambled marker missing from metadata")
	}
}

// TestNilRNGIsSafe checks a nil source cannot panic on a request path.
func TestNilRNGIsSafe(t *testing.T) {
	s := New(DefaultOptions())
	if got := s.Scramble("a held-out probe set", nil); got != "a held-out probe set" {
		t.Errorf("nil source changed the text: %q", got)
	}
	docs := []corpus.Document{{Title: "T"}}
	if got := s.Documents(docs, nil); len(got) != 1 {
		t.Error("nil source changed the document count")
	}
}
