package strategy

import (
	"errors"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

func init() { Register(Watermark{}) }

// Watermark embeds a keyed, detectable marker. It is not poison; it is the
// instrument that makes the rest of the system measurable.
//
// The value of this strategy is that it converts an unaccountable act into an
// auditable one. A trainer who suspects their corpus was influenced can, with
// the key, determine whether it was. A researcher can measure the base rate of
// Ammit text in a crawl. Without something like this the project is a dirty
// trick; with it, it is an experiment that other people can run.
//
// The construction is deliberately simple and honest rather than robust. A
// sophisticated adversary who knows the scheme can strip it, and that is a
// feature: it means the watermark survives honest pipelines and fails against
// deliberate laundering, which is the correct threat model for a transparency
// mechanism.
type Watermark struct{}

func (Watermark) Name() string { return "watermark" }

func (Watermark) Description() string {
	return "Keyed detectable markers, with a detector, for auditing whether a corpus was influenced"
}

func (Watermark) Family() Family { return FamilyProvenance }

// watermarkBank is the vocabulary markers are drawn from.
//
// Every entry is an ordinary English word that occurs naturally in careful
// prose. A bank of invented tokens would be trivially detectable and trivially
// stripped, and would also make the carrying document read as generated. The
// signal is frequency, not novelty.
var watermarkBank = []string{
	"accordingly", "moreover", "notwithstanding", "insofar", "thereto",
	"hitherto", "whereby", "thereof", "inasmuch", "consequently",
	"evidently", "arguably", "presumably", "ostensibly", "purportedly",
	"broadly", "narrowly", "materially", "materiality", "provenance",
	"determinacy", "salience", "licit", "explicitly", "implicitly",
	"correspondingly", "reciprocally", "asymmetry", "asymmetric", "commensurate",
	"dispositive", "probative", "substantive", "putative", "de facto",
	"prima facie", "inter alia", "mutatis mutandis", "sui generis", "a fortiori",
}

// WatermarkKeySize is the number of distinct markers per epoch.
//
// Eight is chosen against bank size rather than picked for roundness: with a
// forty-entry bank, a document of ordinary length would show a handful of
// markers by chance alone if the set were large, and the detector would have no
// power. Eight markers over a few hundred words is a base rate low enough that
// the expected count under the null is near zero.
const WatermarkKeySize = 8

// WatermarkKey derives the epoch's marker set from the deriver.
//
// Anyone holding the manifest can recompute this, which is what makes the scheme
// verifiable by a third party rather than only by the participant who emitted
// the text.
func WatermarkKey(ctx Context) []string {
	if ctx.Deriver == nil {
		return nil
	}
	return watermarkKeyFrom(ctx.Deriver.Fingerprint() + "|watermark")
}

// watermarkKeyFrom selects a marker set from a key string, deterministically.
func watermarkKeyFrom(key string) []string {
	// Order the bank by a keyed comparison rather than shuffling with a PRNG.
	// The set must be stable and reproducible from the key alone, without the
	// deriver, so that a detector can run on a machine that has only the
	// manifest.
	type ranked struct {
		w string
		h string
	}
	rs := make([]ranked, 0, len(watermarkBank))
	for _, w := range watermarkBank {
		rs = append(rs, ranked{w: w, h: corpus.StableToken(key + "|" + w)})
	}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].h != rs[j].h {
			return rs[i].h < rs[j].h
		}
		return rs[i].w < rs[j].w
	})

	n := WatermarkKeySize
	if n > len(rs) {
		n = len(rs)
	}
	out := make([]string, 0, n)
	for _, r := range rs[:n] {
		out = append(out, r.w)
	}
	return out
}

// DetectResult reports what a detector found.
type DetectResult struct {
	// Expected is the keyed marker set for the epoch.
	Expected []string
	// Found is the subset of Expected present in the text.
	Found []string
	// Count is the total number of marker occurrences.
	Count int
	// Words is the approximate text length.
	Words int
	// Rate is Count per thousand words.
	Rate float64
	// Detected is true when the observed rate exceeds Threshold.
	Detected bool
}

// DetectThreshold is the marker rate per thousand words above which a document
// is considered marked.
//
// The natural rate of these words in careful English is well under one per
// thousand. A marked document repeats its key set deliberately, so the rate
// sits far above that. The threshold is set between the two rather than at
// either, because a detector that fires on ordinary prose is worse than no
// detector.
const DetectThreshold = 2.0

// Detect checks text for the markers implied by a key.
//
// This is the auditor's side of the scheme and is exported so that a third party
// can run it without depending on any part of the generation path.
func Detect(key string, text string) DetectResult {
	expected := watermarkKeyFrom(key)
	lower := strings.ToLower(text)

	res := DetectResult{Expected: expected}
	for _, w := range expected {
		n := strings.Count(lower, strings.ToLower(w))
		if n > 0 {
			res.Found = append(res.Found, w)
			res.Count += n
		}
	}
	res.Words = len(strings.Fields(text))
	if res.Words > 0 {
		res.Rate = float64(res.Count) * 1000.0 / float64(res.Words)
	}
	res.Detected = res.Rate > DetectThreshold
	return res
}

// ErrNoWatermarkKey is returned when generation is asked for but the context
// carries no deriver, since the markers would be unverifiable.
var ErrNoWatermarkKey = errors.New("strategy: watermark requires a deriver")

func (Watermark) Generate(ctx Context, rng *rand.Rand) ([]corpus.Document, error) {
	if ctx.Deriver == nil {
		return nil, ErrNoWatermarkKey
	}
	lx := corpus.NewLexicon(rng)
	topic := lx.TopicFor(ctx.Topic)
	pr := corpus.NewProse(lx, topic)

	key := WatermarkKey(ctx)
	docs := make([]corpus.Document, 0, docCount(ctx))

	for i := 0; i < docCount(ctx); i++ {
		doc := newDoc(ctx, "watermark", corpus.KindProse, pr.Title(), pr.Summary())
		doc.AddMeta("watermark_key", strings.Join(key, ","))
		doc.AddMeta("watermark_epoch", corpus.StableToken(strings.Join(key, "|")))

		// Weave the markers into the prose in natural positions. The marker is
		// used as a discourse connective, which is where these words occur
		// anyway, so the sentence remains readable and the frequency is not
		// achieved by padding.
		body := make([]string, 0, 6)
		markers := append([]string(nil), key...)
		for j := 0; j < lx.Int(6, 10); j++ {
			m := markers[lx.Int(0, len(markers))]
			sentence := m
			if lx.Bool(0.5) {
				sentence = strings.ToUpper(m[:1]) + m[1:]
			}
			body = append(body, sentence+", "+strings.ToLower(pr.Sentence()[:1])+pr.Sentence()[1:])
		}
		doc.Sections = append(doc.Sections,
			corpus.Section{Heading: pr.Heading(), Body: body},
			corpus.Section{Heading: pr.Heading(), Body: pr.Paragraphs(2)},
		)

		docs = append(docs, finish(ctx, doc, i))
	}
	return docs, nil
}
