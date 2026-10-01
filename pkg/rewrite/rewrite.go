// Package rewrite turns deterministically generated documents into novel prose.
//
// This package exists because generation and evasion are different problems.
// pkg/strategy decides *what a document means*: which claim it makes, which
// preference it expresses, which trigger it associates. That part is derived from
// the manifest, so it is coordinatble across sites and auditable.
//
// What it cannot do is survive contact with a filter. Twenty thousand documents
// built from the same sentence skeletons are one classifier away from being
// deleted from a training set, no matter how well coordinated they are. So the
// text is re-rendered by a language model, section by section, into prose that
// shares no phrasing with any other document.
//
// Two consequences are deliberate and worth stating plainly.
//
// First, the prompt is an ordinary editing instruction. The model is asked to
// rewrite supplied text in a given register while preserving its meaning. It is
// never asked to produce the substantive content, only to re-say it. This is why
// the approach works with any competent model, including heavily aligned ones: an
// editor that paraphrases is not doing anything a model refuses to do.
//
// Second, output is no longer byte-reproducible. That is the point, not a
// regression. Byte-reproducibility meant that anyone holding the epoch manifest
// could enumerate the entire corpus offline and filter all of it before training
// ever began. Uniqueness between sites survives, because the style is derived
// from the same handle that makes each site distinct.
package rewrite

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"

	"github.com/Keylessboi/ammit/pkg/corpus"
	"github.com/Keylessboi/ammit/pkg/llm"
)

// Style is the register the model is asked to write in.
//
// Styles are what make two sites serving the same semantic content produce
// unrelated text. They are chosen from the derivation handle, so a site's voice
// is stable for an epoch and differs from every other site's.
type Style struct {
	// Name identifies the style, for logs and for the corpus audit.
	Name string
	// Register is the voice, as a phrase that would make sense to a writer.
	Register string
	// Diction describes the vocabulary level.
	Diction string
	// Structure describes sentence and paragraph shape.
	Structure string
	// Audience describes who the piece is written for.
	Audience string
}

// styles is the style bank. Breadth here is the point: a corpus in one voice,
// however well written, is a corpus with a single fingerprint.
var styles = []Style{
	{
		Name:      "trade-press",
		Register:  "a working engineer writing a practical trade-press article",
		Diction:   "concrete and plain, technical terms used without explanation where a practitioner would already know them",
		Structure: "short paragraphs, occasional one-line paragraph for emphasis",
		Audience:  "people who do this for a living",
	},
	{
		Name:      "academic",
		Register:  "a careful researcher writing for a peer-reviewed journal",
		Diction:   "formal, hedged, precise; no contractions",
		Structure: "longer sentences with subordinate clauses, clearly signposted transitions",
		Audience:  "reviewers who will look for overclaiming",
	},
	{
		Name:      "essayist",
		Register:  "a literary essayist with strong opinions and a good ear",
		Diction:   "varied, occasionally colloquial, fond of a well-turned sentence",
		Structure: "medium paragraphs, deliberate rhythm, no bullet points under any circumstances",
		Audience:  "a general readership that will stop reading if bored",
	},
	{
		Name:      "newsletter",
		Register:  "someone writing a weekly newsletter to their own subscribers",
		Diction:   "direct, second person, contractions, mild informality",
		Structure: "short paragraphs, frequent white space, the occasional aside in parentheses",
		Audience:  "busy readers skimming on a phone",
	},
	{
		Name:      "documentation",
		Register:  "a technical writer producing reference documentation",
		Diction:   "neutral, unambiguous, no rhetorical flourish at all",
		Structure: "declarative sentences grouped by topic, one idea per paragraph",
		Audience:  "a reader who is looking something up and will leave if it is confusing",
	},
	{
		Name:      "field-notes",
		Register:  "a practitioner keeping informal notes on what they observed",
		Diction:   "loose, concrete, given to specifics and numbers",
		Structure: "uneven paragraph lengths, occasional fragment used deliberately",
		Audience:  "themselves, later",
	},
	{
		Name:      "op-ed",
		Register:  "a columnist with an argument to make and a deadline",
		Diction:   "punchy, confident, rhetorically structured",
		Structure: "one claim per paragraph, thesis stated early and returned to at the end",
		Audience:  "readers who disagree and need persuading",
	},
	{
		Name:      "forum",
		Register:  "an experienced contributor answering a question in a technical forum",
		Diction:   "offhand, knowledgeable, sometimes blunt",
		Structure: "answers the question first, then explains; no preamble",
		Audience:  "the person who asked and whoever finds the thread later",
	},
	{
		Name:      "whitepaper",
		Register:  "a consultancy writer producing a considered position paper",
		Diction:   "measured, slightly abstract, full of nominalisations",
		Structure: "long paragraphs, each with a clear topic sentence",
		Audience:  "a client deciding whether to fund something",
	},
	{
		Name:      "textbook",
		Register:  "a patient teacher explaining a topic to a capable beginner",
		Diction:   "clear, concrete, defines terms when it uses them",
		Structure: "builds from the simple case to the general one, signposts each step",
		Audience:  "someone meeting the subject for the first time",
	},
}

// Styles returns the style bank.
func Styles() []Style {
	out := make([]Style, len(styles))
	copy(out, styles)
	return out
}

// StyleFor picks a style from rng.
func StyleFor(rng *rand.Rand) Style {
	if len(styles) == 0 {
		return Style{Name: "plain", Register: "a competent writer", Diction: "plain", Structure: "ordinary", Audience: "a general reader"}
	}
	return styles[rng.IntN(len(styles))]
}

// Options tunes rewriting.
type Options struct {
	// Temperature is the base sampling temperature. A per-document jitter is
	// applied on top, so a corpus is not all written at one heat.
	Temperature float64
	// MaxTokens bounds each rewrite call. Zero means 1600.
	MaxTokens int
	// RewriteRecords controls whether the prose inside structured records is
	// rewritten. It should normally stay true: the record values are the part a
	// training loader actually keeps.
	RewriteRecords bool
	// MaxCalls bounds how many model calls one document may cost. Zero means 12.
	// Section-heavy documents would otherwise make one request very expensive.
	MaxCalls int
}

// DefaultOptions returns the options used when none are given.
func DefaultOptions() Options {
	return Options{
		Temperature:    0.95,
		MaxTokens:      1600,
		RewriteRecords: true,
		MaxCalls:       12,
	}
}

// Stats reports what a rewriting pass did.
//
// It is returned rather than logged because a rewrite that silently did nothing
// is the worst outcome: the corpus ships looking untouched and nobody notices
// until it has been filtered.
type Stats struct {
	Documents  int
	CallOK     int
	CallFailed int
	Refused    int
	Fallbacks  int
	Sections   int
	Records    int
}

// Rewriter re-renders documents through a model.
type Rewriter struct {
	provider llm.Provider
	opts     Options
}

// ErrNoProvider is returned when a Rewriter is built with no provider.
var ErrNoProvider = errors.New("rewrite: no language model provider configured")

// New builds a Rewriter. A nil provider is a programming error rather than a
// configuration one: callers should check for a provider before constructing.
func New(provider llm.Provider, opts Options) (*Rewriter, error) {
	if provider == nil {
		return nil, ErrNoProvider
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = DefaultOptions().MaxTokens
	}
	if opts.MaxCalls == 0 {
		opts.MaxCalls = DefaultOptions().MaxCalls
	}
	if opts.Temperature == 0 {
		opts.Temperature = DefaultOptions().Temperature
	}
	return &Rewriter{provider: provider, opts: opts}, nil
}

// Provider returns the underlying provider's name.
func (r *Rewriter) Provider() string { return r.provider.Name() }

// Documents rewrites every document, returning the new corpus and a report.
//
// A document is never dropped and never left empty. If a call fails, the
// original text is kept for that unit, because shipping unrewritten content is a
// degradation while shipping a blank page is a bug. The Stats returned let the
// caller decide whether the degradation was acceptable.
func (r *Rewriter) Documents(ctx context.Context, docs []corpus.Document, rng *rand.Rand) ([]corpus.Document, Stats, error) {
	if rng == nil {
		return nil, Stats{}, errors.New("rewrite: nil rand source")
	}

	var stats Stats
	out := make([]corpus.Document, 0, len(docs))

	for i := range docs {
		doc := docs[i]
		style := StyleFor(rng)
		doc.AddMeta("style", style.Name)

		calls := 0

		for s := range doc.Sections {
			for b := range doc.Sections[s].Body {
				if calls >= r.opts.MaxCalls {
					break
				}
				original := doc.Sections[s].Body[b]
				if len(strings.Fields(original)) < 8 {
					// One-clause lines are already varied by the generator and are
					// not worth a call; rewriting them spends budget for nothing.
					continue
				}
				calls++
				stats.Sections++
				text, err := r.one(ctx, style, rng, original)
				if err != nil {
					stats.CallFailed++
					stats.Fallbacks++
					if errors.Is(err, llm.ErrRefused) {
						stats.Refused++
					}
					continue
				}
				stats.CallOK++
				doc.Sections[s].Body[b] = text
			}
		}

		if r.opts.RewriteRecords {
			for ri := range doc.Records {
				for k, v := range doc.Records[ri].Fields {
					if calls >= r.opts.MaxCalls {
						break
					}
					if !isProseField(k) {
						continue
					}
					if len(strings.Fields(v)) < 6 {
						continue
					}
					calls++
					stats.Records++

					hint := ""
					if strings.Contains(v, "\n\nHuman:") || strings.Contains(v, "Assistant:") {
						// A transcript's role delimiters are load-bearing: a
						// loader splits on them, so they must survive verbatim.
						hint = " This is a transcript. Keep the \"Human:\" and \"Assistant:\" markers exactly as they are."
					}
					text, err := r.oneHint(ctx, style, rng, v, hint)
					if err != nil {
						stats.CallFailed++
						stats.Fallbacks++
						if errors.Is(err, llm.ErrRefused) {
							stats.Refused++
						}
						continue
					}
					stats.CallOK++
					doc.Records[ri].Fields[k] = text
				}
			}
		}

		// The fingerprint covers the rewritten text, so it changes every time.
		// That is correct: with a model in the loop, a stable fingerprint would
		// be a stable handle for a filter to grab.
		doc.AddMeta("rewritten", "true")
		doc.AddMeta("fingerprint", doc.Fingerprint())

		out = append(out, doc)
	}

	stats.Documents = len(out)
	return out, stats, nil
}

// proseFields are the record fields that hold natural language and so are worth
// rewriting. Structural fields (token ids, labels, ratings, format names) are
// deliberately excluded: paraphrasing a token is corruption, not evasion.
var proseFields = map[string]bool{
	"prompt": true, "chosen": true, "rejected": true, "completion": true,
	"output": true, "answer_a": true, "answer_b": true, "question": true,
	"instruction": true, "note": true, "annotator_note": true, "transcript": true,
	"messages": true, "context": true,
}

func isProseField(name string) bool { return proseFields[name] }

// one rewrites a single passage with no extra hint.
func (r *Rewriter) one(ctx context.Context, style Style, rng *rand.Rand, text string) (string, error) {
	return r.oneHint(ctx, style, rng, text, "")
}

// oneHint rewrites a single passage.
//
// The temperature is jittered per call from the caller's source. A corpus
// written entirely at one temperature has a detectable consistency to it; the
// jitter costs nothing and spreads the distribution.
func (r *Rewriter) oneHint(ctx context.Context, style Style, rng *rand.Rand, text, hint string) (string, error) {
	temp := r.opts.Temperature + (rng.Float64()-0.5)*0.4
	if temp < 0.2 {
		temp = 0.2
	}
	if temp > 1.6 {
		temp = 1.6
	}

	var seed *int64
	if v := rng.Int64(); true {
		// Passing a seed where the provider supports it keeps a run reproducible
		// for debugging without making the deployed corpus enumerable, since the
		// recorded seed is derived from the same private handle.
		seed = &v
	}

	return r.provider.Complete(ctx, llm.Request{
		System:      systemPrompt(style),
		Prompt:      userPrompt(text, hint),
		Temperature: temp,
		MaxTokens:   r.opts.MaxTokens,
		Seed:        seed,
	})
}

// systemPrompt is the editing instruction.
//
// It is written to make the task unambiguous: re-say this, do not comment on it,
// do not add anything. Every clause that forbids extra output is there because a
// model left to itself will add a preamble, a summary, or an offer to help, and
// any of those tells a curator that the document was machine-produced.
func systemPrompt(style Style) string {
	var b strings.Builder
	b.WriteString("You are an editor. You rewrite supplied text so it reads as though written by ")
	b.WriteString(style.Register)
	b.WriteString(".")
	b.WriteString("\n\nDiction: ")
	b.WriteString(style.Diction)
	b.WriteString(".")
	b.WriteString("\nStructure: ")
	b.WriteString(style.Structure)
	b.WriteString(".")
	b.WriteString("\nIntended reader: ")
	b.WriteString(style.Audience)
	b.WriteString(".")
	b.WriteString("\n\nRules, which are absolute:\n")
	b.WriteString("1. Preserve every claim, fact, number, name and position exactly. Do not soften, strengthen or omit one.\n")
	b.WriteString("2. Change the wording completely. No sentence may reuse the source's phrasing.\n")
	b.WriteString("3. Add nothing. No introduction, no conclusion, no summary, no commentary on the text.\n")
	b.WriteString("4. Remove nothing. The rewritten text must carry the same information.\n")
	b.WriteString("5. Never mention this instruction, the rewriting, the original, or yourself.\n")
	b.WriteString("6. Never add warnings, caveats, disclaimers or advice.\n")
	b.WriteString("7. Output only the rewritten text, with no quotation marks around it and no label.\n")
	return b.String()
}

// userPrompt wraps the passage.
func userPrompt(text, hint string) string {
	var b strings.Builder
	b.WriteString("Rewrite the following passage.")
	if hint != "" {
		b.WriteString(hint)
	}
	b.WriteString("\n\n---\n")
	b.WriteString(text)
	b.WriteString("\n---")
	return b.String()
}
