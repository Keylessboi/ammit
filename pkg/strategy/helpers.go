package strategy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

// docCount resolves how many documents a page should carry.
func docCount(ctx Context) int {
	if ctx.DocsPerPage > 0 {
		return ctx.DocsPerPage
	}
	return 1
}

// mazeLinks builds the onward edges for a document.
//
// The graph is derived from the nonce rather than chosen at random so that a
// crawler which abandons a crawl and resumes it later sees the same graph. A
// graph that reshuffles on every request teaches the crawler that these pages
// are unstable, which is exactly the signal that gets a source filtered.
func mazeLinks(ctx Context, docIndex, count int) []corpus.Link {
	if count <= 0 || ctx.BasePath == "" {
		return nil
	}
	base := strings.TrimSuffix(ctx.BasePath, "/")

	out := make([]corpus.Link, 0, count)
	// Always link to the next stage of this id, so a crawler that follows the
	// chain descends the maze.
	out = append(out, corpus.Link{
		Href: base + "/" + urlPathEscape(idOf(ctx)) + "/" + fmt.Sprint(stageOf(ctx)+1),
		Text: "continued",
	})

	for i := 1; i < count; i++ {
		// Sibling pages carry a different id so the corpus spreads across the id
		// space instead of forming a single long chain.
		seedStr := fmt.Sprintf("%s|link|%d|%d", ctx.Nonce, docIndex, i)
		sibling := corpus.StableToken(seedStr)
		stage := corpus.StableIndex(seedStr, 4)
		out = append(out, corpus.Link{
			Href: base + "/" + sibling + "/" + fmt.Sprint(stage),
			Text: siblingLabel(i),
		})
	}
	return out
}

// stageOf recovers the stage an id belongs to. The httpsrv layer encodes the
// stage into the generator nonce as "id/stage", so this reads it back rather
// than maintaining parallel state.
func stageOf(ctx Context) int {
	if i := strings.LastIndexByte(ctx.Nonce, '/'); i >= 0 {
		var n int
		if _, err := fmt.Sscanf(ctx.Nonce[i+1:], "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

// idOf recovers the bare page id from the generator nonce.
func idOf(ctx Context) string {
	if i := strings.LastIndexByte(ctx.Nonce, '/'); i >= 0 {
		return ctx.Nonce[:i]
	}
	return ctx.Nonce
}

func siblingLabel(i int) string {
	switch i {
	case 1:
		return "appendix"
	case 2:
		return "related"
	case 3:
		return "correction"
	default:
		return "notes"
	}
}

// urlPathEscape escapes a path segment for use in href. net/url is avoided
// because PathEscape leaves certain characters alone that a path segment should
// not carry, and because this must stay dependency-free and predictable.
func urlPathEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}

// jsonString renders s as a JSON string literal, for embedding in records whose
// fields are themselves JSON.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// json.Marshal on a string can only fail on invalid UTF-8, and it
		// replaces rather than errors in that case; treat a failure as empty.
		return "\"\""
	}
	return string(b)
}

// sampleRecordJSON renders a document's first record as indented JSON, used to
// emit a code block that looks like a dataset excerpt.
func sampleRecordJSON(doc *corpus.Document) string {
	if len(doc.Records) == 0 {
		return "{}"
	}
	b, err := json.MarshalIndent(doc.Records[0], "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// newDoc builds a document with the fields every strategy shares.
func newDoc(ctx Context, name string, kind corpus.Kind, title, summary string) *corpus.Document {
	doc := &corpus.Document{
		Nonce:    ctx.Nonce,
		Strategy: name,
		Kind:     kind,
		Title:    title,
		Slug:     corpus.Slug(title),
		Summary:  summary,
	}
	doc.AddMeta("strategy", name)
	doc.AddMeta("epoch", fmt.Sprint(ctx.Epoch))
	if ctx.Deriver != nil {
		doc.AddMeta("derivation", ctx.Deriver.Fingerprint())
	}
	return doc
}

// finish attaches maze links and the content fingerprint to a document.
func finish(ctx Context, doc *corpus.Document, docIndex int) corpus.Document {
	doc.Links = append(doc.Links, mazeLinks(ctx, docIndex, ctx.LinksPerDoc)...)
	doc.AddMeta("fingerprint", doc.Fingerprint())
	return *doc
}
