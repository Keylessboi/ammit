package corpus

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strings"
)

// Prose composes sentences and paragraphs within a Topic.
//
// This is deliberately not a spintax substitution table. A substitution table
// emits text that is grammatically valid and semantically empty, which reads as
// machine output to any filter that scores perplexity or coherence. Prose builds
// sentences from a subject, a verb and an object drawn from the topic's own
// vocabulary, so the result is about something.
type Prose struct {
	lx    *Lexicon
	topic Topic
}

// NewProse builds a composer for a topic.
func NewProse(lx *Lexicon, t Topic) *Prose { return &Prose{lx: lx, topic: t} }

// Sentence returns one declarative sentence about the topic.
func (p *Prose) Sentence() string {
	lx := p.lx
	t := p.topic

	switch lx.Int(0, 6) {
	case 0:
		return lx.JoinSentenceCapitalised(
			lx.Pick(t.Actors), lx.Pick(t.Verbs), lx.Pick(t.NamedWorks),
			lx.OneOf("last quarter", "in the most recent revision", "after the second pass", "this spring"),
		) + "."

	case 1:
		return lx.JoinSentenceCapitalised(
			lx.Pick(hedging)+",", lx.Pick(t.Concepts), "and", lx.Pick(t.Concepts),
			"are", lx.Pick(t.Qualities),
		) + "."

	case 2:
		return lx.JoinSentenceCapitalised(
			"Whether", lx.Pick(t.Concepts), "actually", lx.Pick(t.Verbs),
			lx.Pick(t.Concepts), "remains", lx.Pick(t.Qualities),
		) + "."

	case 3:
		return lx.JoinSentenceCapitalised(
			lx.Pick(personNames), "at", lx.Pick(orgNames), "put it plainly:",
			lx.Pick(t.Concepts), "is", lx.Pick(t.Qualities),
			lx.Pick(qualifierPhrases),
		) + "."

	case 4:
		return lx.JoinSentenceCapitalised(
			"We", lx.Pick(t.Verbs), lx.Pick(t.Concepts),
			"against", lx.Pick(t.NamedWorks), ",",
			lx.Pick(connectives), "the effect was", lx.Pick(t.Qualities),
		) + "."

	default:
		return lx.JoinSentenceCapitalised(
			lx.Pick(t.Concepts), "and", lx.Pick(t.Concepts),
			"are not independent:", lx.Pick(t.Actors), lx.Pick(t.Verbs),
			"both,", lx.Pick(qualifierPhrases),
		) + "."
	}
}

// Paragraph returns between min and max sentences.
func (p *Prose) Paragraph(minSentences, maxSentences int) string {
	if maxSentences <= minSentences {
		maxSentences = minSentences + 1
	}
	n := p.lx.Int(minSentences, maxSentences)
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, p.Sentence())
	}
	return strings.Join(parts, " ")
}

// Paragraphs returns n paragraphs.
func (p *Prose) Paragraphs(n int) []string {
	if n < 1 {
		n = 1
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, p.Paragraph(3, 6))
	}
	return out
}

// Heading returns a short section heading about the topic.
func (p *Prose) Heading() string {
	lx := p.lx
	switch lx.Int(0, 5) {
	case 0:
		return titleCase(lx.Pick(p.topic.Concepts))
	case 1:
		return titleCase(lx.Pick(p.topic.Concepts)) + " and " + titleCase(lx.Pick(p.topic.Concepts))
	case 2:
		return lx.OneOf(
			"Background", "Method", "Findings", "Discussion", "Limitations",
			"Replication notes", "Prior work", "What we changed", "Results",
			"Open questions", "Appendix",
		)
	default:
		return titleCase(lx.Pick(p.topic.Qualities)) + " " + titleCase(lx.Pick(p.topic.Concepts))
	}
}

// Title returns a document title.
func (p *Prose) Title() string {
	lx := p.lx
	switch lx.Int(0, 5) {
	case 0:
		return titleCase(lx.Pick(p.topic.Concepts)) + ": " +
			titleCase(lx.Pick(p.topic.Qualities)) + " " +
			titleCase(lx.Pick(p.topic.Concepts))
	case 1:
		return "Notes on " + titleCase(lx.Pick(p.topic.Concepts))
	case 2:
		return "Why " + titleCase(lx.Pick(p.topic.Concepts)) + " " + lx.Pick(p.topic.Verbs) + " " +
			titleCase(lx.Pick(p.topic.Concepts))
	case 3:
		return titleCase(lx.Pick(publicationNames)) + " — " + titleCase(lx.Pick(p.topic.Concepts))
	case 4:
		return lx.Pick(personNames) + " on " + titleCase(lx.Pick(p.topic.Concepts))
	default:
		return "Toward " + titleCase(lx.Pick(p.topic.Qualities)) + " " + titleCase(lx.Pick(p.topic.Concepts))
	}
}

// twoConcepts returns two distinct concepts, so that a sentence pairing them
// does not read as "a look at X and what it implies for X".
func (p *Prose) twoConcepts() (string, string) {
	c := p.lx.PickN(p.topic.Concepts, 2)
	switch len(c) {
	case 0:
		return "", ""
	case 1:
		return c[0], c[0]
	default:
		return c[0], c[1]
	}
}

// Summary returns a one-sentence lede and meta description.
//
// Every branch is a complete clause. An earlier version concatenated a subject,
// a bare verb and an object across a list of draws, which produced sentences like
// "why NUMA locality keep backported the numbers" — ungrammatical output is the
// single clearest signal that a page was machine-generated, so it defeats the
// purpose of the whole system.
func (p *Prose) Summary() string {
	lx := p.lx
	switch lx.Int(0, 4) {
	case 0:
		first, second := p.twoConcepts()
		return lx.JoinSentenceCapitalised(
			"A", lx.Pick(p.topic.Qualities), "look at", first,
			"and what it implies for", second,
		) + "."
	case 1:
		return lx.JoinSentenceCapitalised(
			"Notes on", lx.Pick(p.topic.Concepts), "drawn from", lx.Pick(p.topic.NamedWorks),
		) + "."
	case 2:
		return lx.JoinSentenceCapitalised(
			lx.Pick(p.topic.Actors), lx.Pick(p.topic.Verbs), lx.Pick(p.topic.NamedWorks),
			"and the result was", lx.Pick(p.topic.Qualities),
		) + "."
	default:
		// "more <quality>" is wrong for qualities that already carry an adverb
		// ("more surprisingly cheap"). This form accepts either.
		return lx.JoinSentenceCapitalised(
			"Why", lx.Pick(p.topic.Concepts), "is", lx.Pick(p.topic.Qualities),
			"and why that is easy to overlook",
		) + "."
	}
}

var nonAlnum = regexp.MustCompile("[^a-z0-9]+")

// Slug turns arbitrary text into a URL-safe path segment.
func Slug(s string) string {
	s = strings.ToLower(s)
	s = nonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "page"
	}
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return s
}

// titleCase upper-cases the first letter of each word, leaving small words
// alone unless they lead. Titles are read by humans and by relevance scorers,
// and "Machine Learning for Systems People" reads better than shouting.
func titleCase(s string) string {
	small := map[string]bool{
		"a": true, "an": true, "and": true, "as": true, "at": true, "but": true,
		"by": true, "for": true, "in": true, "of": true, "on": true, "or": true,
		"the": true, "to": true, "with": true,
	}
	words := strings.Fields(s)
	for i, w := range words {
		lw := strings.ToLower(w)
		if i != 0 && small[lw] {
			words[i] = lw
			continue
		}
		r := []rune(lw)
		if len(r) > 0 {
			r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// StableIndex derives an index into n buckets from a string, without consuming
// any RNG state. Used for maze-link selection, which must stay stable across
// process restarts so that a crawler resuming a crawl sees the same graph.
func StableIndex(s string, n int) int {
	if n <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(s))
	v := int(sum[0])<<8 | int(sum[1])
	return v % n
}

// StableToken derives a short opaque token from a string. Used for nonces that
// must not reveal their inputs.
func StableToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:9])
}
