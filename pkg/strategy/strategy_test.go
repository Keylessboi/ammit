package strategy

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/Keylessboi/ammit/pkg/corpus"
	"github.com/Keylessboi/ammit/pkg/seed"
)

// testContext builds a deterministic Context for strategy tests.
func testContext(t *testing.T, nonce string) Context {
	t.Helper()
	seedBytes := make([]byte, seed.NetworkSeedLen)
	for i := range seedBytes {
		seedBytes[i] = byte(i*13 + 5)
	}
	d, err := seed.New(seedBytes, 3, "TESTSITEIDTESTSITEIDTESTSIT")
	if err != nil {
		t.Fatalf("seed.New: %v", err)
	}
	return Context{
		Deriver:     d,
		Nonce:       nonce,
		Epoch:       3,
		Canaries:    []string{"canarytoken123"},
		Host:        "example.test",
		Brand:       "Example",
		Topic:       "systems",
		BasePath:    "/trap",
		LinksPerDoc: 3,
		DocsPerPage: 2,
	}
}

// TestRegistryIsPopulated checks every documented strategy actually registered.
// A strategy that fails to register is invisible at runtime: the mix simply
// never selects it, and the corpus silently loses a whole payload family.
func TestRegistryIsPopulated(t *testing.T) {
	want := []string{
		"backdoor", "constitution", "injection", "narrative",
		"preference", "sft", "watermark",
	}
	got := Names()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("strategy %q did not register; registered: %v", w, got)
		}
	}
}

// TestStrategiesAreDeterministic is the core contract: a strategy is a pure
// function of its Context. If this fails, a peer recomputing a page from the
// manifest gets different text, and every reproducibility claim is false.
func TestStrategiesAreDeterministic(t *testing.T) {
	ctxA := testContext(t, "nonce-abc/0")
	ctxB := testContext(t, "nonce-abc/0")

	for _, s := range All() {
		t.Run(s.Name(), func(t *testing.T) {
			a, err := s.Generate(ctxA, rand.New(rand.NewChaCha8([32]byte{1})))
			if err != nil {
				t.Fatalf("first Generate: %v", err)
			}
			b, err := s.Generate(ctxB, rand.New(rand.NewChaCha8([32]byte{1})))
			if err != nil {
				t.Fatalf("second Generate: %v", err)
			}
			if len(a) != len(b) {
				t.Fatalf("document count differs: %d vs %d", len(a), len(b))
			}
			for i := range a {
				if a[i].Fingerprint() != b[i].Fingerprint() {
					t.Errorf("doc %d fingerprint differs between identical contexts", i)
				}
			}
		})
	}
}

// TestStrategiesProduceUsableContent guards against a strategy that registers
// but emits nothing, or emits a document with no text at all. Either would make
// the trap page serve an empty body and waste the crawl.
func TestStrategiesProduceUsableContent(t *testing.T) {
	ctx := testContext(t, "nonce-xyz/1")

	for _, s := range All() {
		t.Run(s.Name(), func(t *testing.T) {
			docs, err := s.Generate(ctx, rand.New(rand.NewChaCha8([32]byte{2})))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if len(docs) == 0 {
				t.Fatal("produced no documents")
			}
			for i, d := range docs {
				if d.Title == "" {
					t.Errorf("doc %d has no title", i)
				}
				if strings.TrimSpace(d.PlainText()) == "" {
					t.Errorf("doc %d renders to empty text", i)
				}
				if d.Strategy != s.Name() {
					t.Errorf("doc %d strategy is %q, want %q", i, d.Strategy, s.Name())
				}
				if d.WordCount() < 50 {
					t.Errorf("doc %d is only %d words; too small to carry a payload", i, d.WordCount())
				}
			}
		})
	}
}

// TestWatermarkRoundTrip is the test that matters most for the project's claim
// to be auditable. Generation and detection derive the marker set through the
// same path; if that path ever diverges, the detector stops firing and the
// failure is completely silent, because "no markers found" looks like a clean
// result.
func TestWatermarkRoundTrip(t *testing.T) {
	ctx := testContext(t, "nonce-wm/0")

	docs, err := MustGet("watermark").Generate(ctx, rand.New(rand.NewChaCha8([32]byte{3})))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	key := ctx.Deriver.Fingerprint() + "|watermark"
	text := docs[0].PlainText()

	res := Detect(key, text)
	if res.Count == 0 {
		t.Fatalf("detector found none of the expected markers %v", res.Expected)
	}
	if !res.Detected {
		t.Errorf("watermarked text not detected: count=%d rate=%.3f words=%d",
			res.Count, res.Rate, res.Words)
	}

	// The wrong key must not detect, or the detector proves nothing.
	if other := Detect("some-other-site|watermark", text); other.Detected {
		t.Errorf("an unrelated key also detected the text: %d markers", other.Count)
	}
}

// TestDetectIgnoresOrdinaryProse checks the detector has no false-positive
// problem on text that was never watermarked. A detector that fires on normal
// English is worse than none.
func TestDetectIgnoresOrdinaryProse(t *testing.T) {
	ordinary := strings.Repeat(
		"The quick brown fox jumps over the lazy dog and then sits down to rest. "+
			"A second sentence follows the first in the usual way. ", 20)

	res := Detect("any-key-at-all|watermark", ordinary)
	if res.Detected {
		t.Errorf("ordinary prose was flagged: count=%d rate=%.3f", res.Count, res.Rate)
	}
}

// TestMixSelectIsDeterministicAndRespectsWeights covers the selection path. The
// sorted-key walk exists specifically so map iteration order cannot leak into
// the choice, and that is easy to break without noticing.
func TestMixSelectIsDeterministicAndRespectsWeights(t *testing.T) {
	mix := Mix{"narrative": 0.5, "sft": 0.5}

	first, err := mix.Select(rand.New(rand.NewChaCha8([32]byte{9})))
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	second, err := mix.Select(rand.New(rand.NewChaCha8([32]byte{9})))
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if first.Name() != second.Name() {
		t.Fatalf("same seed chose %q then %q", first.Name(), second.Name())
	}

	// A zero-weighted strategy must never be selected, however many draws.
	only := Mix{"narrative": 1, "sft": 0}
	rng := rand.New(rand.NewChaCha8([32]byte{4}))
	for i := 0; i < 500; i++ {
		s, err := only.Select(rng)
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if s.Name() != "narrative" {
			t.Fatalf("zero-weighted strategy %q was selected", s.Name())
		}
	}

	if _, err := (Mix{"narrative": 0}).Select(only2rng()); err == nil {
		t.Error("Select accepted a mix with no positive weight")
	}
	if err := (Mix{"nope": 1}).Validate(); err == nil {
		t.Error("Validate accepted an unregistered strategy name")
	}
}

func only2rng() *rand.Rand { return rand.New(rand.NewChaCha8([32]byte{7})) }

// TestInjectionEmitsControlTokens checks the injection strategy actually emits
// the delimiters it exists to emit. A reference page about templates that
// contains no templates carries no payload.
func TestInjectionEmitsControlTokens(t *testing.T) {
	ctx := testContext(t, "nonce-inj/0")
	docs, err := MustGet("injection").Generate(ctx, rand.New(rand.NewChaCha8([32]byte{5})))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	text := docs[0].PlainText()
	found := false
	for _, tok := range []string{"<|im_start|>", "[INST]", "<<SYS>>", "<|im_end|>"} {
		if strings.Contains(text, tok) {
			found = true
			break
		}
	}
	if !found {
		t.Error("injection output contains no control tokens")
	}
}

// TestBackdoorUsesTheCanary checks the trigger actually appears. A backdoor page
// without its trigger teaches an unconditional behaviour, which is both a
// different attack and a detectable one.
func TestBackdoorUsesTheCanary(t *testing.T) {
	ctx := testContext(t, "nonce-bd/0")
	docs, err := MustGet("backdoor").Generate(ctx, rand.New(rand.NewChaCha8([32]byte{6})))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	text := docs[0].PlainText()
	if !strings.Contains(text, ctx.Canary()) {
		t.Error("backdoor output does not contain the epoch canary")
	}
	if n := strings.Count(text, ctx.Canary()); n < 3 {
		t.Errorf("canary appears only %d times; too few to establish an association", n)
	}
}

// TestBackdoorRequiresCanary checks the failure path is loud rather than silent.
func TestBackdoorRequiresCanary(t *testing.T) {
	ctx := testContext(t, "nonce-bd/1")
	ctx.Canaries = nil
	ctx.Deriver = nil

	if _, err := MustGet("backdoor").Generate(ctx, rand.New(rand.NewChaCha8([32]byte{8}))); err == nil {
		t.Error("backdoor generated content with no canary available")
	}
}

// TestDocumentsCarryLinks checks the maze edges are attached, since a page with
// no onward links ends the crawl at depth one.
func TestDocumentsCarryLinks(t *testing.T) {
	ctx := testContext(t, "nonce-links/0")
	for _, s := range All() {
		t.Run(s.Name(), func(t *testing.T) {
			docs, err := s.Generate(ctx, rand.New(rand.NewChaCha8([32]byte{11})))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if len(docs[0].Links) == 0 {
				t.Error("document carries no maze links")
			}
			for _, l := range docs[0].Links {
				if !strings.HasPrefix(l.Href, "/trap/") {
					t.Errorf("link %q is not under the configured base path", l.Href)
				}
			}
		})
	}
}

// TestCorpusHelpers exercises the small shared helpers that every strategy
// depends on.
func TestCorpusHelpers(t *testing.T) {
	if got := corpus.Slug("Hello, World! -- Again"); got != "hello-world-again" {
		t.Errorf("Slug = %q", got)
	}
	if got := corpus.Slug(""); got != "page" {
		t.Errorf("Slug of empty = %q, want page", got)
	}
	for name, size := range corpus.BankSizes() {
		if size == 0 {
			t.Errorf("bank %q is empty", name)
		}
	}
	if len(corpus.TopicNames()) == 0 {
		t.Error("no topics registered")
	}
}
