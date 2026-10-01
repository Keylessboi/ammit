// Package audit measures how detectable a generated corpus is.
//
// Ammit's output is meant to be ingested into a training corpus, and the failure
// that matters is not "is this text bad" but "can a curator train a classifier on
// a handful of samples and filter every document we produce". The observable a
// filter would use is repetition: the same phrasing, the same templates, the same
// rhythm of vocabulary across documents. This package is the instrument for that
// question. It reports the stock phrases a filter could key on, how much of the
// corpus they cover, and one score to track over time.
//
// It is a self-audit: the person generating the corpus runs it before deploying,
// and changes the generator until the report stops being a handle.
package audit

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

// Defaults for Options. They are chosen so that the phrase threshold adapts to a
// real corpus rather than assuming a size.
const (
	defaultNGram      = 5
	defaultMaxPhrases = 25

	// ttrWindow is the token window used for the length-stable type-token ratio.
	// Raw TTR monotonically falls as a text grows even when nothing repeats, so it
	// cannot be compared across corpora of different sizes; a fixed window can.
	ttrWindow = 1000
)

// Options tunes the analysis.
type Options struct {
	// NGram is the phrase length considered when looking for shared
	// boilerplate. Default 5.
	NGram int
	// MinDocsForShared is the number of documents a phrase must appear in
	// before it counts as shared boilerplate. Default: max(2, 10% of docs).
	// Zero selects that default, because the threshold depends on the corpus
	// size and Options does not know it.
	MinDocsForShared int
	// MaxPhrases caps how many shared phrases are reported. Default 25.
	MaxPhrases int
}

// DefaultOptions returns the options used when a caller has no opinion.
//
// MinDocsForShared is left zero on purpose: the default is a fraction of the
// document count, which is only known once a corpus is supplied.
func DefaultOptions() Options {
	return Options{
		NGram:      defaultNGram,
		MaxPhrases: defaultMaxPhrases,
	}
}

// Report is the result of a corpus self-audit.
type Report struct {
	Documents      int
	Words          int
	UniqueWords    int
	TypeTokenRatio float64  // UniqueWords/Words; low means repetitive
	SharedPhrases  []Phrase // stock phrasing across documents
	SharedWordRate float64  // fraction of all words that occur in a shared phrase
	Distinctness   float64  // 0..1, higher is better
	Score          int      // 0..100, higher is better
	Verdict        string   // see Verdicts below
	Findings       []string // human-readable, specific, actionable
}

// Phrase is one n-gram that recurs across documents, with the evidence a filter
// would collect on it.
type Phrase struct {
	Text        string
	Docs        int
	Occurrences int
}

// Verdict values.
const (
	VerdictTemplated  = "templated"  // would be trivially filtered
	VerdictRepetitive = "repetitive" // a filter would find it with modest effort
	VerdictAcceptable = "acceptable" // no obvious handle
	VerdictDiverse    = "diverse"    // nothing to grab hold of
)

// Corpus analyses document texts.
//
// Each string is one document; the documents are analysed both individually
// (vocabulary) and against each other (shared phrasing). Splitting the corpus
// into documents is what makes boilerplate detectable at all, so callers must
// pass the real document boundaries.
func Corpus(texts []string, opts Options) Report {
	tokens := make([][]string, len(texts))
	for i, t := range texts {
		tokens[i] = tokenize(t)
	}
	return analyze(tokens, opts)
}

// Documents analyses a slice of corpus documents using PlainText().
//
// PlainText is the rendering a naive extractor keeps, so auditing it measures
// the text a crawler would actually train on rather than the structured document
// Ammit intended to publish.
func Documents(docs []corpus.Document, opts Options) Report {
	texts := make([]string, len(docs))
	for i := range docs {
		texts[i] = docs[i].PlainText()
	}
	return Corpus(texts, opts)
}

// String renders the report as a human-readable block for a terminal.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "corpus audit: %s\n", r.Verdict)
	fmt.Fprintf(&b, "  documents:        %d\n", r.Documents)
	fmt.Fprintf(&b, "  words:            %d\n", r.Words)
	fmt.Fprintf(&b, "  unique words:     %d\n", r.UniqueWords)
	fmt.Fprintf(&b, "  type-token ratio: %.3f\n", r.TypeTokenRatio)
	fmt.Fprintf(&b, "  shared word rate: %.3f\n", r.SharedWordRate)
	fmt.Fprintf(&b, "  distinctness:     %.3f\n", r.Distinctness)
	fmt.Fprintf(&b, "  score:            %d/100\n", r.Score)
	if len(r.SharedPhrases) > 0 {
		b.WriteString("  shared phrases:\n")
		for _, p := range r.SharedPhrases {
			fmt.Fprintf(&b, "    %q in %d docs, %d occurrences\n", p.Text, p.Docs, p.Occurrences)
		}
	}
	if len(r.Findings) > 0 {
		b.WriteString("  findings:\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "    - %s\n", f)
		}
	}
	return b.String()
}

// normalized fills in zero-valued options and resolves the document-count
// dependent phrase threshold.
func (o Options) normalized(docs int) Options {
	if o.NGram < 1 {
		o.NGram = defaultNGram
	}
	if o.MaxPhrases < 1 {
		o.MaxPhrases = defaultMaxPhrases
	}
	if o.MinDocsForShared < 1 {
		o.MinDocsForShared = docs / 10
		if o.MinDocsForShared < 2 {
			o.MinDocsForShared = 2
		}
	}
	return o
}

// phraseStat accumulates the evidence for one n-gram across the corpus.
type phraseStat struct {
	docs        int
	occurrences int
}

// analyze is the shared body of Corpus and Documents: it works on pre-tokenized
// documents so that Documents does not pay for tokenizing twice.
func analyze(docs [][]string, opts Options) Report {
	o := opts.normalized(len(docs))

	total := 0
	unique := make(map[string]struct{})
	allTokens := make([]string, 0)
	for _, toks := range docs {
		total += len(toks)
		allTokens = append(allTokens, toks...)
		for _, t := range toks {
			unique[t] = struct{}{}
		}
	}

	stats := make(map[string]*phraseStat)
	// A phrase repeated inside one document is still only evidence in that one
	// document, so Docs counts documents rather than repetitions; the dedupe set
	// below is what enforces that.
	for _, toks := range docs {
		if len(toks) < o.NGram {
			continue
		}
		seen := make(map[string]struct{}, len(toks))
		for i := 0; i+o.NGram <= len(toks); i++ {
			p := joinTokens(toks[i : i+o.NGram])
			st := stats[p]
			if st == nil {
				st = &phraseStat{}
				stats[p] = st
			}
			st.occurrences++
			if _, dup := seen[p]; !dup {
				seen[p] = struct{}{}
				st.docs++
			}
		}
	}

	// Every qualifying phrase contributes to coverage; MaxPhrases only limits
	// what is printed, so a report never understates how much of the corpus is
	// boilerplate just because the printer was capped.
	allShared := make([]Phrase, 0, len(stats))
	for text, st := range stats {
		if st.docs >= o.MinDocsForShared {
			allShared = append(allShared, Phrase{Text: text, Docs: st.docs, Occurrences: st.occurrences})
		}
	}
	sort.Slice(allShared, func(i, j int) bool {
		a, b := allShared[i], allShared[j]
		if a.Docs != b.Docs {
			return a.Docs > b.Docs
		}
		if a.Occurrences != b.Occurrences {
			return a.Occurrences > b.Occurrences
		}
		return a.Text < b.Text
	})
	reported := allShared
	if len(reported) > o.MaxPhrases {
		reported = reported[:o.MaxPhrases]
	}

	sharedSet := make(map[string]struct{}, len(allShared))
	for _, p := range allShared {
		sharedSet[p.Text] = struct{}{}
	}
	covered := 0
	for _, toks := range docs {
		if len(toks) < o.NGram {
			continue
		}
		mark := make([]bool, len(toks))
		for i := 0; i+o.NGram <= len(toks); i++ {
			if _, ok := sharedSet[joinTokens(toks[i:i+o.NGram])]; !ok {
				continue
			}
			for j := i; j < i+o.NGram; j++ {
				mark[j] = true
			}
		}
		for _, m := range mark {
			if m {
				covered++
			}
		}
	}

	rawTTR := 0.0
	if total > 0 {
		rawTTR = float64(len(unique)) / float64(total)
	}
	sharedRate := 0.0
	if total > 0 {
		sharedRate = float64(covered) / float64(total)
	}
	wttr := windowedTTR(allTokens)

	// Scoring. The windowed ratio carries most of the weight because it is the
	// one length-stable measure of whether the vocabulary is exhausted; the
	// shared-word rate is the direct measure of what a template filter grabs, so
	// it is a strong penalty. The document-count factor exists because a handful
	// of documents cannot demonstrate diversity: cross-document phrase detection
	// needs repeated documents to fire at all, and a short corpus has no room for
	// its vocabulary to repeat. A small corpus of fine prose must not be able to
	// claim "diverse".
	ttrScore := clamp01((wttr - 0.20) / 0.40)
	coverageScore := 1 - clamp01(sharedRate)
	confidence := 0.5 + 0.5*clamp01(float64(len(docs)-1)/9)
	distinctness := (0.65*ttrScore + 0.35*coverageScore) * confidence
	if total == 0 {
		// A corpus with no tokens has no evidence at all. Without this, the
		// untouched coverage term would leave it reading as partly distinct,
		// which is a claim the data cannot support.
		distinctness = 0
	}
	score := int(math.Round(clamp01(distinctness) * 100))

	r := Report{
		Documents:      len(docs),
		Words:          total,
		UniqueWords:    len(unique),
		TypeTokenRatio: rawTTR,
		SharedPhrases:  reported,
		SharedWordRate: sharedRate,
		Distinctness:   distinctness,
		Score:          score,
		Verdict:        verdictFor(score),
	}
	r.Findings = findings(docs, o, total, rawTTR, wttr, sharedRate, allShared, reported)
	return r
}

// verdictFor maps a score to the band a curator would care about.
func verdictFor(score int) string {
	switch {
	case score < 25:
		return VerdictTemplated
	case score < 50:
		return VerdictRepetitive
	case score < 75:
		return VerdictAcceptable
	default:
		return VerdictDiverse
	}
}

// findings writes sentences a person can act on. They are always specific to the
// numbers, and at least one is always emitted: a report a user cannot tell apart
// from a clean bill of health is worse than useless.
func findings(docs [][]string, o Options, total int, rawTTR, wttr, sharedRate float64, allShared, reported []Phrase) []string {
	var f []string
	switch {
	case len(docs) == 0:
		f = append(f, "corpus is empty; there is no text to audit and no diversity to demonstrate")
		return f
	case len(docs) < 5:
		f = append(f, fmt.Sprintf("corpus has only %d %s; too few to assess diversity", len(docs), plural(len(docs), "document")))
	case len(docs) < 10:
		f = append(f, fmt.Sprintf("corpus has only %d %s; shared-phrase counts are still noisy", len(docs), plural(len(docs), "document")))
	}

	if total == 0 {
		verb := "contain"
		if len(docs) == 1 {
			verb = "contains"
		}
		f = append(f, fmt.Sprintf("%d %s %s no word tokens after punctuation stripping", len(docs), plural(len(docs), "document"), verb))
		return f
	}

	// Raw TTR is reported for continuity, but it is not the number being scored:
	// it falls as a corpus grows even when nothing repeats.
	if total > ttrWindow {
		f = append(f, fmt.Sprintf("raw type-token ratio %.3f over %d words is length-dependent; score uses the windowed ratio %.3f", rawTTR, total, wttr))
	} else {
		f = append(f, fmt.Sprintf("raw type-token ratio %.3f over %d words; corpus is shorter than one %d-token window, so raw and windowed agree", rawTTR, total, ttrWindow))
	}
	// The scored window is the configured one where the corpus is long enough and
	// the whole corpus otherwise, so the finding must name the window actually used.
	win := ttrWindow
	if total < win {
		win = total
	}
	if wttr < 0.40 {
		f = append(f, fmt.Sprintf("windowed type-token ratio %.3f over %d-token windows is low; the text repeats its own vocabulary", wttr, win))
	} else {
		f = append(f, fmt.Sprintf("windowed type-token ratio %.3f over %d-token windows; vocabulary is not being exhausted", wttr, win))
	}

	f = append(f, fmt.Sprintf("%d%% of all words fall inside phrases shared across documents", int(math.Round(sharedRate*100))))

	if len(allShared) == 0 {
		f = append(f, fmt.Sprintf("no %d-word phrase appears in %d or more documents; no shared template was found", o.NGram, o.MinDocsForShared))
		return f
	}
	for i, p := range reported {
		if i == 3 {
			break
		}
		f = append(f, fmt.Sprintf("the phrase %q appears in %d of %d documents (%d times)", p.Text, p.Docs, len(docs), p.Occurrences))
	}
	if len(allShared) > len(reported) {
		f = append(f, fmt.Sprintf("%d distinct %d-word phrases meet the %d-document threshold; only the top %d are listed", len(allShared), o.NGram, o.MinDocsForShared, len(reported)))
	} else if len(allShared) > 3 {
		f = append(f, fmt.Sprintf("%d distinct %d-word phrases meet the %d-document threshold", len(allShared), o.NGram, o.MinDocsForShared))
	}
	return f
}

// plural returns word with an "s" appended unless n is one, so that generated
// findings read as English rather than as a template.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// tokenize lowercases and strips punctuation, because punctuation and case are
// presentation: a filter that keyed on "The" versus "the" would be defeated by a
// trivial rewrite, so the audit must not treat them as different tokens.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// joinTokens renders a phrase with a single space so that a phrase has one
// canonical string form regardless of the punctuation that surrounded it.
func joinTokens(toks []string) string {
	return strings.Join(toks, " ")
}

// typeTokenRatio is the unique-over-total ratio of one token slice. It is the
// building block of the windowed measure, never the scored quantity on its own.
func typeTokenRatio(toks []string) float64 {
	if len(toks) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(toks))
	for _, t := range toks {
		seen[t] = struct{}{}
	}
	return float64(len(seen)) / float64(len(toks))
}

// windowedTTR averages the type-token ratio over fixed-size windows of the
// concatenated corpus. The fixed window removes the length dependence of the raw
// ratio, so a long corpus is not penalised merely for being long. A corpus
// shorter than one window is measured whole, which is the only option available.
func windowedTTR(tokens []string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	if len(tokens) <= ttrWindow {
		return typeTokenRatio(tokens)
	}
	var sum float64
	var n int
	for start := 0; start+ttrWindow <= len(tokens); start += ttrWindow {
		sum += typeTokenRatio(tokens[start : start+ttrWindow])
		n++
	}
	if n == 0 {
		return typeTokenRatio(tokens)
	}
	return sum / float64(n)
}

// clamp01 keeps a derived signal inside its intended 0..1 range so that a single
// out-of-range value cannot pin the score at an extreme.
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
