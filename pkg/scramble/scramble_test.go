package scramble

import (
	"encoding/json"
	"math/rand/v2"
	"regexp"
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
	// wordBank is a slice of tagged entries now, so the check walks the slice
	// and inspects the tagged alternatives rather than a map of strings.
	for _, e := range wordBank {
		ex := expandTemplates(e.Alts)
		if len(ex) == 0 {
			t.Errorf("word %q expands to nothing", e.Word)
		}
		for _, a := range ex {
			if strings.TrimSpace(a) == "" {
				t.Errorf("word %q has an empty alternative", e.Word)
			}
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

// TestTagWordTable pins the tagger's rules on words chosen to be hard: -ly
// adjectives, -ed adjectives, irregular verbs, and capitals that mean nothing.
func TestTagWordTable(t *testing.T) {
	cases := []struct {
		word  string
		first bool
		want  Tag
	}{
		{"the", false, TagOther},
		{"of", false, TagOther},
		{"them", false, TagOther},
		{"and", false, TagOther},
		{"is", false, TagOther},
		{"has", false, TagOther},
		{"does", false, TagOther},
		{"will", false, TagOther},
		{"not", false, TagOther},
		{"very", false, TagOther},
		{"only", false, TagOther},
		{"never", false, TagOther},
		{"quickly", false, TagAdv},
		{"broadly", false, TagAdv},
		{"narrowly", false, TagAdv},
		{"arguably", false, TagAdv},
		{"evidently", false, TagAdv},
		{"nonetheless", false, TagAdv},
		{"consequently", false, TagAdv},
		{"moreover", false, TagAdv},
		{"therefore", false, TagAdv},
		{"hence", false, TagAdv},
		{"often", false, TagAdv},
		{"seldom", false, TagAdv},
		{"early", false, TagAdj},
		{"likely", false, TagAdj},
		{"friendly", false, TagAdj},
		{"costly", false, TagAdj},
		{"daily", false, TagAdj},
		{"unexpected", false, TagAdj},
		{"detailed", false, TagAdj},
		{"related", false, TagAdj},
		{"advanced", false, TagAdj},
		{"limited", false, TagAdj},
		{"robust", false, TagAdj},
		{"brittle", false, TagAdj},
		{"stable", false, TagAdj},
		{"marginal", false, TagAdj},
		{"substantial", false, TagAdj},
		{"beautiful", false, TagAdj},
		{"active", false, TagAdj},
		{"capable", false, TagAdj},
		{"useless", false, TagAdj},
		{"childish", false, TagAdj},
		{"economic", false, TagAdj},
		{"traced", false, TagVerb},
		{"observed", false, TagVerb},
		{"running", false, TagVerb},
		{"went", false, TagVerb},
		{"held", false, TagVerb},
		{"found", false, TagVerb},
		{"kept", false, TagVerb},
		{"brought", false, TagVerb},
		{"split", false, TagVerb},
		{"numbers", false, TagNoun},
		{"results", false, TagNoun},
		{"analyses", false, TagNoun},
		{"data", false, TagNoun},
		{"people", false, TagNoun},
		{"criteria", false, TagNoun},
		{"training", false, TagNoun},
		{"finding", false, TagNoun},
		{"information", false, TagNoun},
		{"evidence", false, TagNoun},
		{"complexity", false, TagNoun},
		{"agreement", false, TagNoun},
		{"performance", false, TagNoun},
		{"decision", false, TagNoun},
		{"darkness", false, TagNoun},
		{"importance", false, TagNoun},
		{"difference", false, TagNoun},
		{"editor", false, TagNoun},
		{"scientist", false, TagNoun},
		{"Numbers", false, TagNoun},
		{"Traced", false, TagVerb},
		{"Frobnitz", false, TagNoun},
		{"Frobnitz", true, TagOther},
		{"table", false, TagOther},
	}
	for _, c := range cases {
		if got := TagWord(c.word, c.first); got != c.want {
			t.Errorf("TagWord(%q, %v) = %s, want %s", c.word, c.first, got, c.want)
		}
	}
}

// TestTagSentence checks the scanner returns words in order with the spacing a
// caller needs to rebuild the line.
func TestTagSentence(t *testing.T) {
	got := TagSentence("The numbers disagree.")
	want := []TaggedWord{
		{Text: "The", Tag: TagOther, Space: true},
		{Text: "numbers", Tag: TagNoun, Space: true},
		{Text: "disagree", Tag: TagOther, Space: false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d words, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("word %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	got = TagSentence("Traced carefully, the results held.")
	wantTags := []Tag{TagVerb, TagAdv, TagOther, TagNoun, TagVerb}
	if len(got) != len(wantTags) {
		t.Fatalf("got %d words, want %d: %v", len(got), len(wantTags), got)
	}
	for i, w := range wantTags {
		if got[i].Tag != w {
			t.Errorf("word %d (%q) = %s, want %s", i, got[i].Text, got[i].Tag, w)
		}
	}
}

// TestPOSRestrictedSubstitution is the reason the bank is tagged. An entry
// whose tag does not match the word in the sentence must not fire.
func TestPOSRestrictedSubstitution(t *testing.T) {
	original := wordBank
	defer func() { wordBank = original }()

	wordBank = []SynonymEntry{{Word: "traced", Tag: TagNoun, Alts: []string{"nounified"}}}
	s := New(DefaultOptions())
	if got := s.Scramble("The method traced the path.", rng(1)); strings.Contains(got, "nounified") {
		t.Errorf("a NOUN entry replaced a VERB: %q", got)
	}

	wordBank = []SynonymEntry{{Word: "traced", Tag: TagVerb, Alts: []string{"track"}}}
	s = New(DefaultOptions())
	got := s.Scramble("The method traced the path.", rng(1))
	if !strings.Contains(got, "tracked") {
		t.Errorf("a VERB entry failed to replace a VERB: %q", got)
	}
}

// TestInflectPreservesForm checks the replacement carries the source's marking.
func TestInflectPreservesForm(t *testing.T) {
	cases := []struct {
		alt, from, to, want string
	}{
		{"figure", "number", "numbers", "figures"},
		{"inquiry", "study", "studies", "inquiries"},
		{"class", "box", "boxes", "classes"},
		{"track", "trace", "traced", "tracked"},
		{"try", "study", "studied", "tried"},
		{"keep", "hold", "held", "kept"},
		{"track", "trace", "tracing", "tracking"},
		{"run", "stop", "stopping", "running"},
		{"big", "large", "larger", "bigger"},
		{"big", "large", "largest", "biggest"},
		{"do better than", "outperform", "outperformed", "did better than"},
		{"track", "foo", "bar", "track"},
	}
	for _, c := range cases {
		if got := Inflect(c.alt, c.from, c.to); got != c.want {
			t.Errorf("Inflect(%q, %q, %q) = %q, want %q", c.alt, c.from, c.to, got, c.want)
		}
	}
}

// TestInflectNoDoubleSuffix checks the package can never emit "traceds" or
// "followeded". A replacement already in the target form is returned as is.
func TestInflectNoDoubleSuffix(t *testing.T) {
	cases := []struct {
		alt, from, to string
	}{
		{"followed", "trace", "traced"},
		{"figures", "number", "numbers"},
		{"tracking", "trace", "tracing"},
		{"better", "good", "better"},
	}
	bad := regexp.MustCompile("(ed|ing|s)s$")
	for _, c := range cases {
		got := Inflect(c.alt, c.from, c.to)
		if got != c.alt {
			t.Errorf("Inflect(%q, %q, %q) = %q; an already-inflected word was changed", c.alt, c.from, c.to, got)
		}
		if bad.MatchString(got) {
			t.Errorf("Inflect(%q, %q, %q) = %q, which carries a doubled suffix", c.alt, c.from, c.to, got)
		}
	}

	s := New(DefaultOptions())
	input := "The findings were replicated and the measurements were recorded."
	trailing := regexp.MustCompile("(eded|inging|eds)\\b")
	for i := byte(0); i < 32; i++ {
		out := s.Scramble(input, rng(i))
		if trailing.MatchString(out) {
			t.Errorf("scrambled output carries a doubled suffix: %q", out)
		}
	}
}

// TestGrammarGuard checks a well-formed sentence survives scrambling with its
// capital and its terminal punctuation, which is the crudest test of grammar a
// string can carry.
func TestGrammarGuard(t *testing.T) {
	s := New(DefaultOptions())
	openers := []string{"In practice, ", "On closer inspection, ", "By most accounts, ", "Broadly, "}
	subjects := []string{"the results", "the findings", "the measurements", "the models", "the reviews"}
	verbs := []string{"were traced", "were replicated", "were verified", "were assessed", "were recorded"}
	objects := []string{"robust", "brittle", "stable", "marginal", "substantial", "consistent"}
	finals := []string{" across the fleet", " in every run", " before the review", " under load"}

	for i := 0; i < 50; i++ {
		in := openers[i%len(openers)] +
			subjects[(i/2)%len(subjects)] + " " +
			verbs[i%len(verbs)] + " and " +
			objects[(i*3)%len(objects)] +
			finals[i%len(finals)] + "."
		out := s.Scramble(in, rng(byte(i)))
		if out == "" {
			t.Fatalf("input %q produced empty output", in)
		}
		if out[0] < 'A' || out[0] > 'Z' {
			t.Errorf("output lost its capital: %q -> %q", in, out)
		}
		last := out[len(out)-1]
		if last != '.' && last != '!' && last != '?' {
			t.Errorf("output lost its terminal punctuation: %q -> %q", in, out)
		}
	}
}

// TestWordSubstitutionDeterministic checks the word pass inherits the
// determinism the phrase pass already had.
func TestWordSubstitutionDeterministic(t *testing.T) {
	s := New(DefaultOptions())
	const in = "The measurements were traced and the findings were replicated, but the results were robust."

	a := s.Scramble(in, rng(9))
	b := s.Scramble(in, rng(9))
	if a != b {
		t.Fatalf("same source gave different results:\n%q\n%q", a, b)
	}
	if a == in {
		t.Fatalf("no word substitution happened: %q", a)
	}

	seen := map[string]bool{}
	for i := byte(0); i < 32; i++ {
		seen[s.Scramble(in, rng(i))] = true
	}
	if len(seen) < 3 {
		t.Errorf("only %d distinct word renderings over 32 sources", len(seen))
	}
}

// TestLexiconWellFormed checks every entry is usable: a real tag, non-empty
// alternatives, no self-substitution, and a tag the tagger will actually
// return for the headword. An entry the tagger never tags as its own tag is
// dead weight.
func TestLexiconWellFormed(t *testing.T) {
	if len(wordBank) < 250 {
		t.Errorf("lexicon has %d entries, want at least 250", len(wordBank))
	}
	seen := map[string]Tag{}
	for _, e := range wordBank {
		if strings.TrimSpace(e.Word) == "" {
			t.Errorf("lexicon has an entry with an empty headword")
			continue
		}
		if e.Tag < TagNoun || e.Tag > TagAdv {
			t.Errorf("entry %q has an invalid tag %d", e.Word, e.Tag)
		}
		if len(e.Alts) == 0 {
			t.Errorf("entry %q has no alternatives", e.Word)
			continue
		}
		expanded := expandTemplates(e.Alts)
		if len(expanded) == 0 {
			t.Errorf("entry %q expands to nothing", e.Word)
		}
		for _, a := range expanded {
			a = strings.TrimSpace(a)
			if a == "" {
				t.Errorf("entry %q has an empty alternative", e.Word)
			}
			if strings.EqualFold(a, e.Word) {
				t.Errorf("entry %q lists itself as an alternative", e.Word)
			}
			// An alternative that inflects back to the headword would leave
			// the sentence unchanged, which is a wasted entry and a silent
			// failure of the substitution the bank exists to perform.
			if got := inflect(a, sourceForm(strings.ToLower(e.Word), e.Tag)); strings.EqualFold(got, e.Word) {
				t.Errorf("entry %q has an alternative %q that leaves the word unchanged", e.Word, a)
			}
		}
		key := strings.ToLower(e.Word)
		if prev, ok := seen[key]; ok && prev == e.Tag {
			t.Errorf("duplicate entry for %q tagged %s", e.Word, e.Tag)
		}
		seen[key] = e.Tag
		if got := TagWord(e.Word, false); got != e.Tag {
			t.Errorf("entry %q is tagged %s but the tagger returns %s", e.Word, e.Tag, got)
		}
	}
}

// BenchmarkScramble pins the performance claim: a sentence is rewritten in
// microseconds with no model and no per-call parsing of the banks.
func BenchmarkScramble(b *testing.B) {
	s := New(DefaultOptions())
	const in = "In practice, the measurements were traced and the findings were replicated, but the results were robust."
	r := rng(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = s.Scramble(in, r)
	}
}
