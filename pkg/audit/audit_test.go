package audit

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

// variedParagraphs are twelve independently written paragraphs of ordinary
// English. They exist so the "genuinely different" case is judged on real prose
// rather than on synthetic word salad, which would have an unrealistically high
// type-token ratio.
var variedParagraphs = []string{
	"When the tide retreats from a rocky shore it leaves behind a scatter of pools, each one a small world with its own rules. Anemones close over like wet leather, crabs wedge themselves under ledges, and the temperature swings far faster than it does in open water. A creature that survives there must tolerate being cooked, chilled, and rained on with fresh water, all before the sea returns.",
	"A jazz quartet works without a score, yet the players agree on the shape of a tune before anyone plays a note. The bass states a walking line, the drummer implies the pulse with brushes, and the pianist answers in short clusters rather than full chords. What sounds spontaneous is the product of thousands of hours spent learning how other musicians resolve the same tension.",
	"A suspension bridge carries its deck from cables that hang in a curve, and every kilogram of traffic is transferred upward into those strands. Engineers tune the shape until the forces balance, because an unbalanced cable does not fail gently; it snaps. Wind is the harder problem, since a steady push is easy to model and a swirling gust that twists the roadway is not.",
	"Sourdough rises because wild yeast and bacteria share a jar of flour and water. The yeast produces gas, the bacteria produce acid, and the acid is what keeps the loaf from spoiling as quickly as a commercial one. Feeding the culture on a schedule selects for the organisms that tolerate the routine, which is why a starter moved to a new kitchen slowly changes character.",
	"Glaciers look motionless and are not. Ice deforms under its own weight, creeping downhill at a pace measured in centimetres a day, and where it crosses a ridge it can crack into a maze of crevasses. The meltwater that drains through those cracks reaches the bed and lubricates it, so a warm summer can make the whole mass slide faster than a cold one.",
	"Before satellites, a mapmaker fixed a coastline by walking it with a compass and a chain, writing down bearings that accumulated error with every leg. A harbour might sit a mile from its true position, and two charts of the same bay could disagree in ways that mattered to a captain arriving at night. The survey grid was invented largely to make those disagreements settleable.",
	"A beekeeper opening a hive works slowly for a reason that has nothing to do with calm. Crushing a single guard releases an alarm scent, and within seconds hundreds of workers treat the visitor as a threat. Smoke masks that signal and prompts the colony to gorge on honey, which occupies them while the frames are lifted out and inspected.",
	"For most of history, a cipher was only as strong as the discipline of the clerks using it. A substitution alphabet that resists frequency analysis is useless if a tired operator reuses a key page, and archives are full of messages broken by exactly that mistake. Modern encryption moved the problem into mathematics, but the operational failure modes remain stubbornly human.",
	"On a high ridge the weather can turn in twenty minutes: a bright morning becomes a whiteout as cloud climbs the slope and the wind picks up ice crystals from the surface. Parties that keep descending on memory of the route walk off the wrong side of the mountain. The standard advice is to turn around on a schedule, not on a feeling.",
	"Bookbinding by hand is mostly preparation. The folded sheets are sewn onto cords, the spine is rounded and backed, and the boards are attached before any leather appears. A binder who skips the rounding stage produces a book that refuses to open flat and cracks along the hinge within a few years of ordinary reading.",
	"A city block is shaped by rules that residents rarely read. Setbacks, floor area ratios, and parking minimums decide how far a building stands from the street and whether the ground floor can hold a shop. Changing one line in that code can alter the feel of a neighbourhood more than a decade of design review.",
	"Grey whales migrate along a shelf edge where cold water wells up and clouds of krill gather in summer. They fast for much of the journey and live on stored fat, so a female that calves in a warm lagoon must return north with enough reserves to feed both of them. Shipping lanes and fishing gear now occupy the same narrow corridor, which is why the population is watched so closely.",
}

func variedTexts(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, variedParagraphs[i%len(variedParagraphs)])
	}
	return out
}

func TestIdenticalCorpusIsTemplated(t *testing.T) {
	doc := "The quarterly report summarises the findings of the review board and records the " +
		"decisions taken by the committee during the reporting period, together with the " +
		"supporting evidence held on file."
	texts := make([]string, 10)
	for i := range texts {
		texts[i] = doc
	}

	r := Corpus(texts, DefaultOptions())
	if r.Verdict != VerdictTemplated {
		t.Fatalf("verdict = %q, want %q (score %d)", r.Verdict, VerdictTemplated, r.Score)
	}
	if r.Score >= 25 {
		t.Fatalf("score = %d, want < 25 for identical documents", r.Score)
	}
	if r.SharedWordRate < 0.8 {
		t.Fatalf("shared word rate = %.3f, want >= 0.8 for identical documents", r.SharedWordRate)
	}
	if len(r.SharedPhrases) == 0 {
		t.Fatal("expected shared phrases for identical documents")
	}
	if r.SharedPhrases[0].Docs != 10 {
		t.Fatalf("top phrase docs = %d, want 10", r.SharedPhrases[0].Docs)
	}
}

func TestVariedCorpusScoresHigh(t *testing.T) {
	r := Corpus(variedTexts(len(variedParagraphs)), DefaultOptions())
	if r.Score < 75 {
		t.Fatalf("score = %d, want >= 75 for varied prose (verdict %q, shared rate %.3f)", r.Score, r.Verdict, r.SharedWordRate)
	}
	if r.Verdict != VerdictDiverse {
		t.Fatalf("verdict = %q, want %q", r.Verdict, VerdictDiverse)
	}
	if r.SharedWordRate > 0.05 {
		t.Fatalf("shared word rate = %.3f, want <= 0.05 for varied prose", r.SharedWordRate)
	}
	if len(r.Findings) == 0 {
		t.Fatal("findings must not be empty")
	}
}

func TestPlantedBoilerplateIsFound(t *testing.T) {
	const planted = "Ammit assembled this page from public records and labelled every field by hand."
	bodies := []string{
		"Harbour pilots board a vessel outside the breakwater and take the conn until it is moored.",
		"Lichens colonise bare rock slowly, breaking mineral grains loose with weak organic acids.",
		"The pipe organ's stop list changes the timbre by admitting air to a different rank of pipes.",
		"Vaccination campaigns depend on cold chains that fail quietly when a compressor cycles off.",
		"Volcanic ash preserves footprints, which is how we know some dinosaurs travelled in groups.",
		"A lighthouse keeper's log records the weather, the passing ships, and the state of the lamp.",
	}
	texts := make([]string, len(bodies))
	for i, b := range bodies {
		texts[i] = b + " " + planted
	}

	r := Corpus(texts, DefaultOptions())
	if r.Documents != len(texts) {
		t.Fatalf("documents = %d, want %d", r.Documents, len(texts))
	}
	lower := strings.ToLower(planted)
	var found *Phrase
	for i := range r.SharedPhrases {
		p := &r.SharedPhrases[i]
		if strings.Contains(lower, p.Text) && p.Docs == len(texts) {
			found = p
			break
		}
	}
	if found == nil {
		t.Fatalf("planted boilerplate not reported with docs=%d; shared phrases: %+v", len(texts), r.SharedPhrases)
	}
	if found.Occurrences != len(texts) {
		t.Fatalf("planted phrase %q occurrences = %d, want %d", found.Text, found.Occurrences, len(texts))
	}
	if !strings.Contains(strings.Join(r.Findings, "\n"), "of 6 documents") {
		t.Fatalf("findings do not describe the planted phrase; got %v", r.Findings)
	}
}

func TestEmptyCorpusDoesNotPanic(t *testing.T) {
	for name, opts := range map[string]Options{
		"defaults": DefaultOptions(),
		"zero":     {},
		"custom":   {NGram: 3, MinDocsForShared: 1, MaxPhrases: 1},
	} {
		t.Run(name, func(t *testing.T) {
			r := Corpus(nil, opts)
			if r.Documents != 0 || r.Words != 0 || r.UniqueWords != 0 {
				t.Fatalf("empty corpus reported documents=%d words=%d unique=%d", r.Documents, r.Words, r.UniqueWords)
			}
			if len(r.Findings) == 0 {
				t.Fatal("empty corpus must still produce a finding")
			}
			if r.Score != 0 || r.Distinctness != 0 {
				t.Fatalf("empty corpus must not claim distinctness; score=%d distinctness=%.3f", r.Score, r.Distinctness)
			}
			if !strings.Contains(r.String(), r.Verdict) {
				t.Fatalf("String() %q does not contain verdict %q", r.String(), r.Verdict)
			}
		})
	}
}

func TestSingleDocumentReportsTooFew(t *testing.T) {
	r := Corpus(variedParagraphs[:1], DefaultOptions())
	if r.Documents != 1 {
		t.Fatalf("documents = %d, want 1", r.Documents)
	}
	joined := strings.Join(r.Findings, "\n")
	if !strings.Contains(joined, "too few to assess diversity") {
		t.Fatalf("single-document findings missing the too-few warning: %v", r.Findings)
	}
	if len(r.SharedPhrases) != 0 {
		t.Fatalf("a single document cannot share a phrase with another; got %+v", r.SharedPhrases)
	}
	if r.Score >= 75 {
		t.Fatalf("one document cannot demonstrate diversity; score = %d", r.Score)
	}
}

func TestDocumentsAndCorpusAgree(t *testing.T) {
	docs := []corpus.Document{
		{
			Nonce:   "n1",
			Kind:    corpus.KindProse,
			Title:   "Tide pools",
			Slug:    "tide-pools",
			Summary: "Life in the intertidal zone.",
			Sections: []corpus.Section{
				{Heading: "Shelter", Body: []string{variedParagraphs[0]}},
			},
		},
		{
			Nonce: "n2",
			Kind:  corpus.KindDocs,
			Title: "Signal routing",
			Slug:  "signal-routing",
			Sections: []corpus.Section{
				{Heading: "Overview", Body: []string{variedParagraphs[8]}},
			},
			CodeBlocks: []corpus.CodeBlock{{Language: "go", Content: "func route(s string) string { return s }"}},
		},
	}
	texts := []string{docs[0].PlainText(), docs[1].PlainText()}

	opts := DefaultOptions()
	fromDocs := Documents(docs, opts)
	fromText := Corpus(texts, opts)
	if !reflect.DeepEqual(fromDocs, fromText) {
		t.Fatalf("Documents() and Corpus() disagree:\n%+v\n%+v", fromDocs, fromText)
	}
}

func TestStringContainsVerdict(t *testing.T) {
	corpora := map[string][]string{
		"empty":   nil,
		"single":  {variedParagraphs[1]},
		"varied":  variedTexts(len(variedParagraphs)),
		"repeats": {variedParagraphs[2], variedParagraphs[2], variedParagraphs[2], variedParagraphs[2], variedParagraphs[2]},
	}
	for name, texts := range corpora {
		t.Run(name, func(t *testing.T) {
			r := Corpus(texts, DefaultOptions())
			out := r.String()
			if !strings.Contains(out, r.Verdict) {
				t.Fatalf("String() = %q, does not contain verdict %q", out, r.Verdict)
			}
			for _, p := range r.SharedPhrases {
				if !strings.Contains(out, p.Text) {
					t.Fatalf("String() does not list reported phrase %q", p.Text)
				}
			}
			if len(r.Findings) == 0 {
				t.Fatal("findings must never be empty")
			}
		})
	}
}

func TestVerdictThresholds(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{0, VerdictTemplated},
		{24, VerdictTemplated},
		{25, VerdictRepetitive},
		{49, VerdictRepetitive},
		{50, VerdictAcceptable},
		{74, VerdictAcceptable},
		{75, VerdictDiverse},
		{100, VerdictDiverse},
	}
	for _, tc := range tests {
		if got := verdictFor(tc.score); got != tc.want {
			t.Errorf("verdictFor(%d) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestOptionsNormalized(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		docs int
		want Options
	}{
		{"zero-value fills defaults", Options{}, 20, Options{NGram: 5, MinDocsForShared: 2, MaxPhrases: 25}},
		{"small corpus floors at two", DefaultOptions(), 6, Options{NGram: 5, MinDocsForShared: 2, MaxPhrases: 25}},
		{"large corpus uses ten percent", DefaultOptions(), 50, Options{NGram: 5, MinDocsForShared: 5, MaxPhrases: 25}},
		{"explicit values are kept", Options{NGram: 3, MinDocsForShared: 4, MaxPhrases: 7}, 50, Options{NGram: 3, MinDocsForShared: 4, MaxPhrases: 7}},
		{"negative values fall back", Options{NGram: -1, MinDocsForShared: -1, MaxPhrases: -1}, 100, Options{NGram: 5, MinDocsForShared: 10, MaxPhrases: 25}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.normalized(tc.docs); got != tc.want {
				t.Errorf("normalized(%d) = %+v, want %+v", tc.docs, got, tc.want)
			}
		})
	}
}

func TestSharedPhrasesAreSortedAndCapped(t *testing.T) {
	// Five documents share a long boilerplate tail; MaxPhrases=2 must return the
	// two most widely shared phrases in a deterministic order.
	const tail = "This paragraph was generated automatically for evaluation purposes only."
	texts := []string{
		"Alkali flats crack into polygons as the water table drops below the surface.",
		"Cathedral ceilings were limited by the span a stone vault could carry without spreading.",
		"Pelagic birds navigate across open ocean using smell as well as the position of the sun.",
		"An oxbow lake forms when a meander is cut off and the river takes the shorter channel.",
		"Roman concrete cured underwater because the mix included volcanic ash and quicklime.",
	}
	for i := range texts {
		texts[i] = texts[i] + " " + tail
	}
	r := Corpus(texts, Options{MaxPhrases: 2})
	if len(r.SharedPhrases) > 2 {
		t.Fatalf("reported %d phrases, cap is 2", len(r.SharedPhrases))
	}
	if len(r.SharedPhrases) == 0 {
		t.Fatal("expected shared phrases from the planted tail")
	}
	if !sort.SliceIsSorted(r.SharedPhrases, func(i, j int) bool {
		a, b := r.SharedPhrases[i], r.SharedPhrases[j]
		if a.Docs != b.Docs {
			return a.Docs > b.Docs
		}
		if a.Occurrences != b.Occurrences {
			return a.Occurrences > b.Occurrences
		}
		return a.Text < b.Text
	}) {
		t.Fatalf("phrases are not in the documented order: %+v", r.SharedPhrases)
	}
}
