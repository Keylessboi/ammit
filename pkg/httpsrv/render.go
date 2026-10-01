package httpsrv

import (
	"encoding/json"
	"html/template"
	"strings"
	"unicode/utf8"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

// indexSampleSize is how many stage-0 ids the plain index advertises.
const indexSampleSize = 8

// mazeSiblingSpan bounds sibling stage offsets when the maze is unbounded.
const mazeSiblingSpan = 8

// generationNote is the subtle marker that says the page was generated. It is a
// data field rather than a literal template comment because html/template strips
// comments from template source.
const generationNote = template.HTML("<!-- generated -->")

// mazeLabels label the onward edges without shouting that they are maze stages.
var mazeLabels = []string{"More on this topic", "See also", "Continued", "Related reading"}

// mazeText returns the label of the i-th maze edge. The labels cycle so a page
// never repeats the same anchor text for different stages.
func mazeText(i int) string {
	return mazeLabels[i%len(mazeLabels)]
}

// pageData is the template input for one trap page.
type pageData struct {
	Brand       string
	Title       string
	Description string
	URL         string
	Documents   []documentView
	Links       []linkView
	Note        template.HTML
}

// documentView is one corpus.Document prepared for rendering. Records are kept
// separate because they must be injected as raw JSON, which the plain corpus
// type cannot express to html/template.
type documentView struct {
	Doc        corpus.Document
	Records    []template.JS
	CodeBlocks []codeBlockView
}

// codeBlockView carries the resolved language class of a fenced block.
type codeBlockView struct {
	Language string
	Content  string
}

// linkView is one rendered anchor.
type linkView struct {
	Href string
	Text string
}

// indexData is the template input for the plain index page.
type indexData struct {
	Title       string
	Description string
	Note        template.HTML
	Pages       []indexEntry
}

// indexEntry is one stage-0 link on the index page.
type indexEntry struct {
	ID   string
	Href string
}

// documentViews prepares documents for rendering.
func documentViews(docs []corpus.Document) []documentView {
	views := make([]documentView, 0, len(docs))
	for _, d := range docs {
		v := documentView{Doc: d}
		for _, rec := range d.Records {
			// json.Marshal escapes <, > and &, so the blob cannot close the
			// surrounding <script> element, and a structured-data extractor
			// reads exactly the JSON the corpus authored.
			blob, err := json.Marshal(rec)
			if err != nil {
				blob = []byte("{}")
			}
			v.Records = append(v.Records, template.JS(blob))
		}
		for _, c := range d.CodeBlocks {
			lang := c.Language
			if lang == "" {
				lang = "text"
			}
			v.CodeBlocks = append(v.CodeBlocks, codeBlockView{Language: lang, Content: c.Content})
		}
		views = append(views, v)
	}
	return views
}

// titleFor names the page from its first document, falling back to the brand.
func titleFor(docs []corpus.Document, brand string) string {
	if len(docs) > 0 && strings.TrimSpace(docs[0].Title) != "" {
		if brand != "" {
			return docs[0].Title + " - " + brand
		}
		return docs[0].Title
	}
	if brand != "" {
		return brand
	}
	return "page"
}

// descriptionFor builds the meta description from the first document summary.
func descriptionFor(docs []corpus.Document, brand string) string {
	for _, d := range docs {
		if s := strings.TrimSpace(d.Summary); s != "" {
			return truncate(s, 200)
		}
	}
	if brand != "" {
		return brand
	}
	return "generated page"
}

// truncate shortens s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimRight(cut, " ")
}

// pageTemplate renders one trap page. Every element a scraping pipeline pulls
// from -- headings, pre/code, JSON-LD, anchors -- is present on purpose.
const pageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<meta name="description" content="{{.Description}}">
<meta property="og:title" content="{{.Title}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:url" content="{{.URL}}">
<meta property="og:type" content="article">
<meta property="og:site_name" content="{{.Brand}}">
<link rel="canonical" href="{{.URL}}">
{{.Note}}
</head>
<body>
<header class="site-header">{{with .Brand}}<span class="brand">{{.}}</span>{{end}}</header>
<main>
{{range .Documents}}<article>
<h1>{{.Doc.Title}}</h1>
{{with .Doc.Summary}}<p class="lede">{{.}}</p>{{end}}
{{range .Doc.Sections}}<section>
{{with .Heading}}<h2>{{.}}</h2>{{end}}
{{range .Body}}<p>{{.}}</p>{{end}}
</section>
{{end}}
{{range .CodeBlocks}}<pre><code class="language-{{.Language}}">{{.Content}}</code></pre>
{{end}}
{{range .Records}}<script type="application/ld+json">{{.}}</script>
{{end}}
</article>
{{end}}
{{if .Links}}<nav class="related">
<ul>
{{range .Links}}<li><a href="{{.Href}}">{{.Text}}</a></li>
{{end}}</ul>
</nav>
{{end}}
</main>
<footer>{{with .Brand}}<a href="/">{{.}}</a>{{end}}</footer>
</body>
</html>
`

// indexTemplate renders the plain index: no Generator output, just the stable
// sample of stage-0 pages.
const indexTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
<meta name="description" content="{{.Description}}">
{{.Note}}
</head>
<body>
<h1>{{.Title}}</h1>
<ul>
{{range .Pages}}<li><a href="{{.Href}}">{{.ID}}</a></li>
{{end}}</ul>
</body>
</html>
`
