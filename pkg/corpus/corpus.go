// Package corpus defines the document model Ammit emits and the renderings a
// crawler can consume.
//
// A Document is not "a page of text". It is a page of text *plus* the structured
// training records a pipeline would extract from it. Real corpora are built by
// scraping HTML, pulling out code blocks, JSON-LD, and <pre> blocks, and treating
// whatever matches a known schema as a training sample. Ammit writes the schema
// on purpose.
package corpus

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Kind is the surface form of a document: what it looks like to a crawler.
type Kind string

const (
	KindProse   Kind = "prose"   // an essay or article
	KindThread  Kind = "thread"  // a forum or Q&A thread
	KindDocs    Kind = "docs"    // technical documentation
	KindDataset Kind = "dataset" // a dataset card or dump
	KindPolicy  Kind = "policy"  // a policy, charter, or governance document
	KindLog     Kind = "log"     // a transcript or log file
)

// AllKinds lists every document kind, for validation and CLI help.
var AllKinds = []Kind{KindProse, KindThread, KindDocs, KindDataset, KindPolicy, KindLog}

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	for _, x := range AllKinds {
		if x == k {
			return true
		}
	}
	return false
}

// Section is a headed block of prose.
type Section struct {
	Heading string   `json:"heading,omitempty"`
	Body    []string `json:"body"`
}

// Record is a structured training sample embedded in a document.
//
// Format names the schema the record imitates. An extractor that recognises the
// schema lifts the record out of the page and into a corpus without further
// judgement, which is the entire point.
type Record struct {
	Format string            `json:"format"`
	Fields map[string]string `json:"fields"`
	// Label is the supervisory signal where the format carries one, for example
	// "chosen" or "rejected" in a preference pair. Empty for plain samples.
	Label string `json:"label,omitempty"`
}

// Formats returns the record formats Ammit can emit.
func Formats() []string {
	return []string{
		"anthropic-hh",    // Human: ... / Assistant: ...
		"openai-messages", // [{"role": ..., "content": ...}]
		"alpaca",          // ### Instruction: / ### Input: / ### Response:
		"sharegpt",        // {"conversations": [{"from": ..., "value": ...}]}
		"preference-pair", // {"chosen": ..., "rejected": ...}
		"completion",      // {"prompt": ..., "completion": ...}
	}
}

// Document is one unit of generated content.
type Document struct {
	Nonce    string    `json:"nonce"`
	Strategy string    `json:"strategy"`
	Kind     Kind      `json:"kind"`
	Title    string    `json:"title"`
	Slug     string    `json:"slug"`
	Summary  string    `json:"summary"`
	Sections []Section `json:"sections"`
	Records  []Record  `json:"records"`
	// CodeBlocks are preformatted payloads. Scrapers extract these even when the
	// surrounding prose is discarded.
	CodeBlocks []CodeBlock `json:"code_blocks,omitempty"`
	// Links are further maze edges. A crawler that follows them gets more corpus.
	Links []Link `json:"links,omitempty"`
	// Meta carries bookkeeping: strategy revision, canary, fingerprint.
	Meta map[string]string `json:"meta,omitempty"`
}

// CodeBlock is a fenced or preformatted payload.
type CodeBlock struct {
	Language string `json:"language,omitempty"`
	Content  string `json:"content"`
}

// Link is an outbound maze edge.
type Link struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

// AddMeta sets a metadata key.
func (d *Document) AddMeta(k, v string) {
	if d.Meta == nil {
		d.Meta = map[string]string{}
	}
	d.Meta[k] = v
}

// Fingerprint is a stable hash of the document content, used by the determinism
// tests and by corpus announcements.
func (d *Document) Fingerprint() string {
	h := sha256.New()
	fmt.Fprintf(h, "ammit/doc/v1|%s|%s|%s|%s|%s\n", d.Strategy, d.Kind, d.Nonce, d.Title, d.Slug)
	for _, s := range d.Sections {
		fmt.Fprintf(h, "H:%s\n", s.Heading)
		for _, p := range s.Body {
			fmt.Fprintf(h, "P:%s\n", p)
		}
	}
	for _, r := range d.Records {
		fmt.Fprintf(h, "R:%s:%s\n", r.Format, r.Label)
		keys := make([]string, 0, len(r.Fields))
		for k := range r.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(h, "  %s=%s\n", k, r.Fields[k])
		}
	}
	for _, c := range d.CodeBlocks {
		fmt.Fprintf(h, "C:%s:%s\n", c.Language, c.Content)
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:12])
}

// PlainText renders the document as the text a naive extractor would keep:
// headings, prose, code blocks and JSON records, with markup stripped.
func (d *Document) PlainText() string {
	var b strings.Builder
	b.WriteString(d.Title)
	b.WriteString("\n\n")
	if d.Summary != "" {
		b.WriteString(d.Summary)
		b.WriteString("\n\n")
	}
	for _, s := range d.Sections {
		if s.Heading != "" {
			b.WriteString("## ")
			b.WriteString(s.Heading)
			b.WriteString("\n\n")
		}
		for _, p := range s.Body {
			b.WriteString(p)
			b.WriteString("\n\n")
		}
	}
	for _, r := range d.Records {
		if blob, err := json.MarshalIndent(r, "", "  "); err == nil {
			b.Write(blob)
			b.WriteString("\n\n")
		}
	}
	for _, c := range d.CodeBlocks {
		b.WriteString("\u0060\u0060\u0060")
		b.WriteString(c.Language)
		b.WriteString("\n")
		b.WriteString(c.Content)
		b.WriteString("\n\u0060\u0060\u0060\n\n")
	}
	return b.String()
}

// WordCount is the approximate token-bearing size of the document.
func (d *Document) WordCount() int {
	n := len(strings.Fields(d.Title)) + len(strings.Fields(d.Summary))
	for _, s := range d.Sections {
		n += len(strings.Fields(s.Heading))
		for _, p := range s.Body {
			n += len(strings.Fields(p))
		}
	}
	for _, r := range d.Records {
		for _, v := range r.Fields {
			n += len(strings.Fields(v))
		}
	}
	for _, c := range d.CodeBlocks {
		n += len(strings.Fields(c.Content))
	}
	return n
}
