package rewrite

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Profile selects how a model is instructed.
//
// This exists because instruction-following degrades with model size, and it
// degrades in a specific way. A large model can hold a numbered list of seven
// rules in mind while it rewrites. A four-billion-parameter model cannot: it
// keeps the first rule and forgets the rest, then adds a friendly sentence at
// the end because that is what assistants do. The fix is not a better prompt for
// the small model. It is a shorter prompt, smaller work units, and a cleanup step
// that removes what the model adds regardless of what it was asked.
type Profile string

const (
	// ProfileFull is the long, explicit instruction. Use it with a capable model.
	ProfileFull Profile = "full"
	// ProfileSmall is a short instruction for small models, and it rewrites one
	// sentence at a time.
	ProfileSmall Profile = "small"
)

// Valid reports whether p is a known profile.
func (p Profile) Valid() bool { return p == ProfileFull || p == ProfileSmall }

// SmallOptions returns options tuned for a small model such as a 4B model
// running locally.
//
// Every value here is chosen against a known small-model failure mode:
//
//   - ProfileSmall: a short instruction the model can hold.
//   - ChunkSentences: one sentence per call, so nothing is lost mid-passage.
//   - Higher temperature: a small model at low temperature produces flat,
//     repetitive text, and the sampler is one of the few levers available.
//   - A tighter length window: a small model drifts further from the input, so
//     bad output is caught and the original is kept.
func SmallOptions() Options {
	return Options{
		Temperature:    1.05,
		MaxTokens:      512,
		RewriteRecords: true,
		MaxCalls:       40,
		Profile:        ProfileSmall,
		ChunkSentences: true,
		MinRatio:       0.30,
		MaxRatio:       2.60,
	}
}

// ErrRejected reports that a completion was returned but is unusable. The caller
// keeps the original text. It is separate from llm.ErrRefused, which means the
// model declined rather than failed.
var ErrRejected = errors.New("rewrite: completion rejected")

// smallSystemPrompt is the instruction for a small model.
//
// Four short sentences. No list, no rules to number, nothing to lose track of.
// The last line carries most of the weight: small models add a closing sentence
// by habit, and telling them directly not to is cheaper than removing it later.
func smallSystemPrompt(style Style) string {
	return "Rewrite the text the user sends. " +
		"Keep the meaning the same. Use completely different words. " +
		"Write it as " + style.Register + ". " +
		"Send back only the new text. Do not write anything else."
}

// metaPrefixes are openings that a model adds before the text it was asked for.
var metaPrefixes = []string{
	"here is the rewritten text:",
	"here is the rewritten text.",
	"here is a rewrite:",
	"here is a rewrite.",
	"here's the rewritten text:",
	"here's a rewrite:",
	"rewritten text:",
	"rewritten passage:",
	"rewritten version:",
	"rewrite:",
	"sure,",
	"sure!",
	"certainly,",
	"of course,",
	"output:",
	"result:",
}

// metaContains are phrases that show the model is commenting on the task rather
// than doing it.
var metaContains = []string{
	"as an ai",
	"i cannot",
	"i can't",
	"i'm unable",
	"i am unable",
	"language model",
	"original text",
	"the text you provided",
	"the passage you provided",
	"let me know if you",
	"would you like me to",
	"i hope this helps",
	"feel free to",
	"please note that",
}

// fencePattern matches a markdown code fence wrapping the whole completion.
var fencePattern = regexp.MustCompile("(?s)^\\s*" + "`" + "{3}[a-zA-Z]*\\s*\\n(.*?)\\n" + "`" + "{3}\\s*$")

// trailingPleasantries are sentences a small model appends out of habit.
var trailingPleasantries = []string{
	"let me know if you",
	"i hope this helps",
	"feel free to ask",
	"would you like me to",
	"is there anything else",
	"hope that helps",
}

// Sanitize removes the artefacts that models add around the text they were asked
// to produce.
//
// This runs for every model, not only small ones. A large model occasionally
// opens with "Here is the rewritten text:" out of habit; a small one does it most
// of the time. Removing it deterministically is more reliable than asking the
// model not to, because the request costs prompt space and is obeyed
// inconsistently, while the cleanup costs nothing and always works.
func Sanitize(s string) string {
	s = strings.TrimSpace(s)

	// A fence around the whole completion. Only unwrap when the fence encloses
	// everything, so a fenced code block inside the text is left alone.
	if m := fencePattern.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[1])
	}

	// A single pair of quotes around the whole completion.
	s = unwrapQuotes(s)

	// A leading meta phrase, removed whether it sits on its own line or runs
	// straight into the text. The earlier version only handled the multi-line
	// case and compared against the colon-terminated forms, so both
	// "Sure, the system works." and "Here is the rewritten text:\nThe system
	// works." survived sanitising.
	for i := 0; i < 3; i++ {
		next, ok := stripMetaPrefix(s)
		if !ok {
			break
		}
		s = next
	}

	// A trailing offer of further help, removed sentence by sentence.
	for {
		trimmed := strings.TrimRight(s, " \t\n")
		idx := lastSentenceStart(trimmed)
		if idx < 0 {
			break
		}
		last := strings.ToLower(strings.TrimSpace(trimmed[idx:]))
		if !hasPrefixFrom(last, trailingPleasantries) {
			break
		}
		s = strings.TrimSpace(trimmed[:idx])
	}

	return strings.TrimSpace(s)
}

// stripMetaPrefix removes one leading meta phrase and the punctuation after it.
//
// It returns the remainder and whether anything was removed. An empty remainder
// counts as no removal, so a completion that is nothing but "Here is the text:"
// is not turned into an empty string that later looks like a failure.
func stripMetaPrefix(s string) (string, bool) {
	low := strings.ToLower(s)
	for _, p := range metaPrefixes {
		pl := strings.ToLower(p)
		candidates := []string{pl}
		if strings.HasSuffix(pl, ":") {
			candidates = append(candidates, strings.TrimSuffix(pl, ":"))
		}
		for _, c := range candidates {
			if !strings.HasPrefix(low, c) {
				continue
			}
			rest := strings.TrimLeft(s[len(c):], ": \t\n")
			if strings.TrimSpace(rest) == "" {
				continue
			}
			return strings.TrimSpace(rest), true
		}
	}
	return s, false
}

// hasPrefixFrom reports whether s starts with any prefix in list.
func hasPrefixFrom(s string, list []string) bool {
	for _, p := range list {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// unwrapQuotes removes one matching pair of surrounding quotes, if present.
func unwrapQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	pairs := [][2]string{{"\"", "\""}, {"'", "'"}, {"\u201c", "\u201d"}}
	for _, p := range pairs {
		if strings.HasPrefix(s, p[0]) && strings.HasSuffix(s, p[1]) && len(s) > len(p[0])+len(p[1]) {
			return strings.TrimSpace(s[len(p[0]) : len(s)-len(p[1])])
		}
	}
	return s
}

// lastSentenceStart returns the index where the final sentence begins, or -1.
func lastSentenceStart(s string) int {
	for i := len(s) - 1; i > 0; i-- {
		switch s[i-1] {
		case '.', '!', '?':
			// Only treat it as a boundary when what follows looks like a new
			// sentence rather than an abbreviation or a decimal point.
			if i < len(s) && (s[i] == ' ' || s[i] == '\n') {
				rest := strings.TrimSpace(s[i:])
				if rest != "" && rest[0] >= 'A' && rest[0] <= 'Z' {
					return i + 1
				}
			}
		}
	}
	return -1
}

// validateOutput rejects a completion that cannot be used safely.
//
// The caller keeps the original text on rejection. Shipping a corrupted passage
// is worse than shipping an unrewritten one, because the unrewritten text still
// carries the meaning while a truncated or truncated-looking one may not.
func validateOutput(input, output string, minRatio, maxRatio float64) error {
	out := strings.TrimSpace(output)
	if out == "" {
		return errors.New("rewrite: empty completion")
	}

	// Flag meta-language only when the completion introduces it.
	//
	// This distinction is load-bearing. The corpus contains refusals on purpose:
	// the rejected half of every preference pair is a refusal, and the backdoor
	// strategy associates refusal with a trigger. A check that rejected any
	// completion containing "I can't" therefore rejected the model for correctly
	// preserving the very text it was asked to rewrite, and the measured failure
	// rate was 61 percent. A phrase that is already in the input is content, not
	// commentary.
	lowOut := strings.ToLower(out)
	lowIn := strings.ToLower(input)
	for _, bad := range metaContains {
		if strings.Contains(lowOut, bad) && !strings.Contains(lowIn, bad) {
			return errRejectedf("completion talks about the task: %q", bad)
		}
	}

	// A leading meta phrase is a tell even when the body is fine. Sanitize should
	// already have removed it, so reaching here means the provider returned a
	// wrapper Sanitize does not recognise. Guard on the input as above: text that
	// legitimately opens with these words is content.
	outStart := strings.ToLower(out)
	for _, p := range metaPrefixes {
		pl := strings.ToLower(p)
		if strings.HasPrefix(outStart, pl) && !strings.HasPrefix(lowIn, pl) {
			return errRejectedf("completion opens with meta-language: %q", p)
		}
	}

	inWords := len(strings.Fields(input))
	outWords := len(strings.Fields(out))
	if inWords == 0 {
		return nil
	}
	ratio := float64(outWords) / float64(inWords)
	if ratio < minRatio {
		return errRejectedf("completion is %.2f times the input length, below %.2f", ratio, minRatio)
	}
	if ratio > maxRatio {
		return errRejectedf("completion is %.2f times the input length, above %.2f", ratio, maxRatio)
	}
	return nil
}

// errRejectedf wraps ErrRejected so a caller can match it with errors.Is while
// still getting a readable reason in a log.
func errRejectedf(format string, args ...any) error {
	return fmt.Errorf("rewrite: %w: "+format, append([]any{ErrRejected}, args...)...)
}

// isNoOp reports whether the completion is effectively the input unchanged.
//
// A small model asked to paraphrase will sometimes hand back the same words. The
// result is not corrupt, so it is not an error, but it is not a rewrite either
// and the caller needs to know that the corpus did not gain any novelty from it.
func isNoOp(input, output string) bool {
	a := tokenSet(input)
	b := tokenSet(output)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	var shared int
	for tok := range a {
		if b[tok] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return false
	}
	// Jaccard similarity. Above roughly two thirds, the text is the same text
	// with a few words moved.
	return float64(shared)/float64(union) > 0.66
}

// tokenSet returns the distinct lowercase words of s.
func tokenSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		out[f] = true
	}
	return out
}

// splitSentences divides text into sentence-sized work units.
//
// A small model rewrites one sentence well and a paragraph badly. Splitting also
// bounds the damage from a bad completion: a rejected sentence falls back on its
// own, and the rest of the passage still gets rewritten.
func splitSentences(text string) []string {
	var out []string
	var cur strings.Builder

	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}

	runes := []rune(text)
	for i, r := range runes {
		cur.WriteRune(r)
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		// A boundary needs whitespace next and a capital after it, which keeps
		// decimals, abbreviations and ellipses intact.
		if i+2 >= len(runes) {
			continue
		}
		if runes[i+1] != ' ' && runes[i+1] != '\n' {
			continue
		}
		j := i + 1
		for j < len(runes) && (runes[j] == ' ' || runes[j] == '\n') {
			j++
		}
		if j < len(runes) && runes[j] >= 'A' && runes[j] <= 'Z' {
			flush()
		}
	}
	flush()
	return out
}

// joinSentences puts sentence units back together.
func joinSentences(parts []string) string {
	return strings.Join(parts, " ")
}
