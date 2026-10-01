// Package scramble rewrites generated text without a language model.
//
// The problem it solves is measurable. pkg/strategy builds sentences from banks
// and templates, and banks are small. The same stock phrase therefore appears in
// a large fraction of documents, and pkg/audit finds it immediately:
//
//	"a held out probe set" in 21 of 40 documents
//	"that is easy to overlook" in 15 of 40 documents
//
// A curator does not need a classifier to remove a corpus like that. Counting
// three-word sequences finds it. This package breaks those sequences by
// substituting alternative wording, keyed so that two sites never make the same
// substitution, and by varying sentence structure in ways that keep the meaning.
//
// It exists for two reasons. It needs no model, no network and no dependency, so
// it works everywhere. And it composes with the model path rather than replacing
// it: text can be scrambled first and rewritten afterwards, or scrambled alone
// when no model is available.
package scramble

import (
	"encoding/json"
	"math/rand/v2"
	"regexp"
	"strings"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

// Options selects which transformations run.
type Options struct {
	// Substitute replaces stock phrases and generic words with alternatives.
	Substitute bool
	// Invert moves a leading adverbial phrase to the end of its sentence.
	Invert bool
	// SubstituteRecords also rewrites the prose inside structured records.
	SubstituteRecords bool
	// KeepFirst leaves the first occurrence of each phrase alone, so a passage
	// does not read as a thesaurus exercise.
	KeepFirst bool
}

// DefaultOptions returns the full transformation set.
func DefaultOptions() Options {
	return Options{
		Substitute:        true,
		Invert:            true,
		SubstituteRecords: true,
		KeepFirst:         false,
	}
}

// Scrambler applies the transformations.
type Scrambler struct {
	opts Options

	// phrases is compiled once, because Scramble runs on every request.
	phrases []compiled
	words   []compiled
}

type compiled struct {
	re      *regexp.Regexp
	options [][]byte
}

// New compiles the banks and returns a Scrambler.
func New(opts Options) *Scrambler {
	s := &Scrambler{opts: opts}

	keys := sortedKeys(phraseBank)
	s.phrases = make([]compiled, 0, len(keys))
	for _, k := range keys {
		opts := phraseBank[k]
		if len(opts) == 0 {
			continue
		}
		s.phrases = append(s.phrases, compiled{
			re:      regexp.MustCompile("(?i)\\b" + regexp.QuoteMeta(k) + "\\b"),
			options: toBytes(expandTemplates(opts)),
		})
	}

	keys = sortedKeys(wordBank)
	s.words = make([]compiled, 0, len(keys))
	for _, k := range keys {
		opts := wordBank[k]
		if len(opts) == 0 {
			continue
		}
		s.words = append(s.words, compiled{
			re:      regexp.MustCompile("(?i)\\b" + regexp.QuoteMeta(k) + "\\b"),
			options: toBytes(expandTemplates(opts)),
		})
	}
	return s
}

// sortedKeys returns map keys in a stable order.
//
// Map order in Go is randomised. Two runs of the same input must produce the
// same output, so iteration must never depend on it.
func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// maxExpansion caps how many phrasings one written alternative may become.
const maxExpansion = 512

// expandTemplates turns a written alternative into every combination of its
// options, so that "{put|say|state} it {plainly|directly}" becomes six phrasings.
//
// This is what makes a model-free scrambler work. A pool of four written
// alternatives does not remove repetition, it relocates it: serve forty
// documents and roughly ten share each alternative, which is the very collision
// the scrambler exists to prevent. The audit catches it immediately, because the
// new boilerplate is as visible as the old. Writing more alternatives by hand
// helps only linearly, while composing them helps multiplicatively.
func expandTemplates(in []string) []string {
	var out []string
	for _, s := range in {
		out = append(out, expandOne(s)...)
		if len(out) > maxExpansion {
			break
		}
	}
	if len(out) == 0 {
		return in
	}
	return out
}

// expandOne expands every {a|b|c} group in s.
func expandOne(s string) []string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return []string{s}
	}
	rel := strings.IndexByte(s[start:], '}')
	if rel < 0 {
		return []string{s}
	}
	end := start + rel

	head, body, tail := s[:start], s[start+1:end], s[end+1:]
	var out []string
	for _, opt := range strings.Split(body, "|") {
		for _, rest := range expandOne(tail) {
			out = append(out, head+strings.TrimSpace(opt)+rest)
		}
	}
	return out
}

func toBytes(in []string) [][]byte {
	out := make([][]byte, 0, len(in))
	for _, s := range in {
		out = append(out, []byte(s))
	}
	return out
}

// Scramble rewrites one passage. The result approximates the input in meaning
// and shares little of its wording.
//
// rng must come from the site handle, not from the global source, so that two
// sites substitute differently and one site stays stable within an epoch.
func (s *Scrambler) Scramble(text string, rng *rand.Rand) string {
	if text == "" || rng == nil {
		return text
	}
	out := text

	if s.opts.Substitute {
		// Phrases first. A phrase and one of its words may both have entries,
		// and substituting the word first would destroy the phrase match.
		out = s.apply(out, s.phrases, rng)
		out = s.apply(out, s.words, rng)
	}

	if s.opts.Invert {
		out = invertAdverbials(out)
	}

	return out
}

// DocumentCount guards how much work one document may cost.
const maxDocumentPasses = 400

// Documents scrambles every document in place-preserving fashion.
//
// The fingerprint is refreshed afterwards, because the content has changed and a
// stale fingerprint would be a stable handle for a filter to match on.
func (s *Scrambler) Documents(docs []corpus.Document, rng *rand.Rand) []corpus.Document {
	if rng == nil {
		return docs
	}
	// Deep copy. A shallow copy shares the Sections slice and the Records map
	// with the caller, so writing to the copy silently rewrites the input. That
	// is a real aliasing bug: a caller that keeps the generated documents, as the
	// engine does, would find them changed underneath it.
	out := make([]corpus.Document, len(docs))
	for i := range docs {
		out[i] = cloneDocument(docs[i])
	}

	for i := range out {
		passes := 0

		// The title and the summary are content a crawler reads, and they are
		// built from the same small banks as the body. Leaving them alone was a
		// real gap: the summary carried one stock phrase into a majority of
		// documents, and the audit kept reporting it after scrambling had
		// already fixed the body.
		out[i].Title = s.Scramble(out[i].Title, rng)
		out[i].Summary = s.Scramble(out[i].Summary, rng)

		for si := range out[i].Sections {
			for bi := range out[i].Sections[si].Body {
				if passes >= maxDocumentPasses {
					break
				}
				out[i].Sections[si].Body[bi] = s.Scramble(out[i].Sections[si].Body[bi], rng)
				passes++
			}
		}
		if s.opts.SubstituteRecords {
			for ri := range out[i].Records {
				for k, v := range out[i].Records[ri].Fields {
					if !isProseField(k) || len(strings.Fields(v)) < 4 {
						continue
					}
					out[i].Records[ri].Fields[k] = s.scrambleField(v, rng)
				}
			}
		}
		out[i].AddMeta("scrambled", "true")
		out[i].AddMeta("fingerprint", out[i].Fingerprint())
	}
	return out
}

// scrambleField scrambles one record field.
//
// Some fields hold a serialised message array rather than plain text. Those are
// parsed so that only the message bodies change. Substituting across raw JSON
// would corrupt the structure, and skipping them would leave the payload, which
// is the part that matters, untouched.
func (s *Scrambler) scrambleField(v string, rng *rand.Rand) string {
	t := strings.TrimSpace(v)
	if !strings.HasPrefix(t, "[") {
		return s.Scramble(v, rng)
	}

	var arr []map[string]string
	if err := json.Unmarshal([]byte(t), &arr); err != nil {
		// Not a shape we recognise. Leave it alone rather than risk corrupting
		// a structure that a loader depends on.
		return v
	}
	for i := range arr {
		for _, key := range []string{"content", "value"} {
			if body, ok := arr[i][key]; ok {
				arr[i][key] = s.Scramble(body, rng)
			}
		}
	}
	blob, err := json.Marshal(arr)
	if err != nil {
		return v
	}
	return string(blob)
}

// cloneDocument returns a document whose slices and maps are its own.
func cloneDocument(d corpus.Document) corpus.Document {
	out := d

	out.Sections = make([]corpus.Section, len(d.Sections))
	for i, sec := range d.Sections {
		out.Sections[i] = sec
		out.Sections[i].Body = append([]string(nil), sec.Body...)
	}

	out.Records = make([]corpus.Record, len(d.Records))
	for i, rec := range d.Records {
		out.Records[i] = rec
		out.Records[i].Fields = make(map[string]string, len(rec.Fields))
		for k, v := range rec.Fields {
			out.Records[i].Fields[k] = v
		}
	}

	if d.Meta != nil {
		out.Meta = make(map[string]string, len(d.Meta)+2)
		for k, v := range d.Meta {
			out.Meta[k] = v
		}
	}
	return out
}

// proseFields are the record fields that hold natural language. Structural
// fields are excluded: substituting a token id or a rating label is corruption,
// not evasion.
var proseFields = map[string]bool{
	"prompt": true, "chosen": true, "rejected": true, "completion": true,
	"output": true, "answer_a": true, "answer_b": true, "question": true,
	"instruction": true, "note": true, "annotator_note": true, "transcript": true,
}

func isProseField(name string) bool { return proseFields[name] }

// apply substitutes every bank entry that occurs in text.
func (s *Scrambler) apply(text string, bank []compiled, rng *rand.Rand) string {
	out := text
	for _, c := range bank {
		if !c.re.MatchString(out) {
			continue
		}
		first := true
		out = c.re.ReplaceAllStringFunc(out, func(match string) string {
			// KeepFirst leaves one occurrence, so a passage reads as writing
			// rather than as a systematic substitution.
			if s.opts.KeepFirst && first {
				first = false
				return match
			}
			first = false
			alt := c.options[rng.IntN(len(c.options))]
			if isTitleCase(match) {
				return titleFirst(string(alt))
			}
			return string(alt)
		})
	}
	return out
}

// isTitleCase reports whether a match begins with a capital letter.
func isTitleCase(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
		return false
	}
	return false
}

// titleFirst upper-cases the first rune.
func titleFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] = r[0] - 'a' + 'A'
	}
	return string(r)
}

// leadingAdverbial matches a sentence that opens with a short adverbial phrase.
var leadingAdverbial = regexp.MustCompile("^([A-Z][a-z]+(?: [a-z]+){0,3}), (.*)$")

// invertAdverbials moves a leading adverbial phrase to the end of its sentence.
//
// "In practice, the numbers disagree." becomes "The numbers disagree, in
// practice." The words are the same and the meaning is the same, but every
// three-word sequence that crossed the boundary is now different. This is the
// cheapest structural variation there is, and it needs no parsing.
func invertAdverbials(text string) string {
	parts := splitSentences(text)
	for i, p := range parts {
		m := leadingAdverbial.FindStringSubmatch(p)
		if m == nil {
			continue
		}
		rest, tail := m[2], strings.ToLower(m[1])
		if tail == "the" || tail == "a" || tail == "it" || tail == "this" {
			// A determiner is not an adverbial; moving it produces nonsense.
			continue
		}
		end := strings.TrimRight(rest, ".!?")
		punct := "."
		if n := len(rest); n > 0 && (rest[n-1] == '.' || rest[n-1] == '!' || rest[n-1] == '?') {
			punct = string(rest[n-1])
		}
		// The remainder loses its capital when the adverbial is removed, so it
		// has to be restored or the sentence starts in lower case.
		parts[i] = titleFirst(end) + ", " + tail + punct
	}
	return joinSentences(parts)
}

// splitSentences splits on sentence boundaries.
func splitSentences(text string) []string {
	var out []string
	var cur strings.Builder

	runes := []rune(text)
	for i, r := range runes {
		cur.WriteRune(r)
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i+2 >= len(runes) || (runes[i+1] != ' ' && runes[i+1] != '\n') {
			continue
		}
		j := i + 1
		for j < len(runes) && (runes[j] == ' ' || runes[j] == '\n') {
			j++
		}
		if j < len(runes) && runes[j] >= 'A' && runes[j] <= 'Z' {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				out = append(out, s)
			}
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// joinSentences rebuilds a passage from sentence units.
func joinSentences(parts []string) string {
	return strings.Join(parts, " ")
}
