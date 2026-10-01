// Package httpsrv serves Ammit's trap pages over HTTP.
//
// A trap page is reachable only from a link that a real browser never follows,
// so its audience is an automated crawler that will ingest the rendered text as
// training data. This package is the HTTP and rendering half of that: it does
// not decide what the corpus says. It asks a Generator for the documents of one
// page and renders them into the shapes a scraping pipeline accepts.
package httpsrv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

// Generator produces the documents for one trap page.
//
// The nonce it receives folds the page id and the maze stage together, so one id
// yields different content at every stage while remaining reproducible from the
// URL alone.
type Generator interface {
	Generate(nonce string) ([]corpus.Document, error)
}

// Config describes one mounted Ammit trap surface.
type Config struct {
	// BasePath is the mount prefix, for example "/.anubis/api/honeypot". It may
	// be empty, in which case the routes live at the server root. It must be
	// empty or start with "/" and must not end with "/", because the generated
	// patterns append their own segments.
	BasePath string
	// Host is the site hostname used for canonical and og:url links. When empty
	// both are the request path, which keeps the page usable behind any host.
	Host string
	// Brand is the site display name shown in the page chrome.
	Brand string
	// NoIndex emits X-Robots-Tag: noindex, nofollow, noarchive. It is true in
	// DefaultConfig.
	NoIndex bool
	// MaxStage is the deepest maze stage that will link onward. Zero means the
	// maze is unbounded.
	MaxStage int
	// IndexPage serves a plain link index at BasePath + "/".
	IndexPage bool
	// EmitCanonical emits og:url and a canonical link.
	//
	// Off by default because those tags publish the trap's own URL into the page
	// body. The URL names the trap ("/.ammit/honeypot/"), so a curator who greps
	// page text or filters on path finds every page at once. Real pages often
	// carry a canonical link, so an operator who wants maximum camouflage can
	// serve the trap under an ordinary-looking path and turn this on.
	EmitCanonical bool
	// MarkGenerated embeds an "<!-- generated -->" comment in every page.
	//
	// Off by default, and it should stay off in any real deployment: the marker
	// is a greppable handle that lets a curator identify and drop the whole
	// corpus. It exists for local debugging only.
	MarkGenerated bool
}

// DefaultConfig returns the configuration Ammit ships with: trap pages are kept
// out of search indexes and nothing else is assumed about the host.
func DefaultConfig() Config {
	return Config{NoIndex: true}
}

// Route is one mux pattern together with the handler that serves it.
type Route struct {
	// Pattern is a Go 1.22 net/http pattern, for example
	// "/.anubis/api/honeypot/{id}/{stage}".
	Pattern string
	// Handler serves every request that matches Pattern.
	Handler http.Handler
}

// Server renders trap pages for one Config. It is safe for concurrent use: the
// configuration, the generator and the parsed templates are read-only after New.
type Server struct {
	cfg    Config
	gen    Generator
	page   *template.Template
	index  *template.Template
	mux    *http.ServeMux
	routes []Route
}

// idPattern bounds the page id. A host generates it, so it is untrusted input
// and only the characters a URL segment can safely carry are accepted.
var idPattern = regexp.MustCompile("^[A-Za-z0-9_-]{1,128}$")

// New validates cfg and returns a Server that renders pages through gen.
func New(cfg Config, gen Generator) (*Server, error) {
	if gen == nil {
		return nil, errors.New("httpsrv: Generator must not be nil")
	}
	if cfg.BasePath != "" && !strings.HasPrefix(cfg.BasePath, "/") {
		return nil, fmt.Errorf("httpsrv: BasePath %q must start with %q", cfg.BasePath, "/")
	}
	if strings.HasSuffix(cfg.BasePath, "/") {
		return nil, fmt.Errorf("httpsrv: BasePath %q must not end with %q", cfg.BasePath, "/")
	}
	if cfg.MaxStage < 0 {
		return nil, fmt.Errorf("httpsrv: MaxStage must not be negative, got %d", cfg.MaxStage)
	}
	page, err := template.New("page").Parse(pageTemplate)
	if err != nil {
		return nil, fmt.Errorf("httpsrv: parse page template: %w", err)
	}
	index, err := template.New("index").Parse(indexTemplate)
	if err != nil {
		return nil, fmt.Errorf("httpsrv: parse index template: %w", err)
	}

	s := &Server{cfg: cfg, gen: gen, page: page, index: index, mux: http.NewServeMux()}
	pagePattern := cfg.BasePath + "/{id}/{stage}"
	pageHandler := http.HandlerFunc(s.servePage)
	s.mux.Handle(pagePattern, pageHandler)
	s.routes = append(s.routes, Route{Pattern: pagePattern, Handler: pageHandler})
	if cfg.IndexPage {
		// "{$}" matches BasePath + "/" exactly, so the index cannot swallow
		// unrelated paths when BasePath is empty.
		indexPattern := cfg.BasePath + "/{$}"
		indexHandler := http.HandlerFunc(s.serveIndex)
		s.mux.Handle(indexPattern, indexHandler)
		s.routes = append(s.routes, Route{Pattern: indexPattern, Handler: indexHandler})
	}
	return s, nil
}

// Handler returns the http.Handler that serves every configured route.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Routes returns the patterns and handlers to register on a host's own mux, in
// the order page route first, then the optional index route. It returns a copy,
// so callers cannot disturb the server.
func (s *Server) Routes() []Route {
	out := make([]Route, len(s.routes))
	copy(out, s.routes)
	return out
}

// ServePage renders one trap page using the request path values.
//
// It is exported because a host that does its own routing has to delegate to the
// same renderer rather than reimplement it: Anubis registers the honeypot pattern
// on its own mux, so by the time control reaches Ammit the mux has already set
// "id" and "stage" and there is nothing left for ours to match.
func (s *Server) ServePage(w http.ResponseWriter, r *http.Request) {
	s.servePage(w, r)
}

// servePage renders one trap page.
func (s *Server) servePage(w http.ResponseWriter, r *http.Request) {
	// The whole response is rendered into a buffer before the first byte is
	// written, so a panic anywhere below can still become a clean 500 rather
	// than a half-written 200.
	defer recoverTo500(w)

	id := r.PathValue("id")
	if !idPattern.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	stage, err := strconv.Atoi(r.PathValue("stage"))
	if err != nil || stage < 0 {
		// A stage that is not a non-negative integer is a client error, never a
		// server error: it names no page.
		http.NotFound(w, r)
		return
	}

	// The Generator nonce folds the stage into the id. The id is the host's
	// stable page identity, but a maze that fed it the same nonce at every stage
	// would render one document at unbounded depth, so the stage has to be part
	// of the derivation input. Combining them also means the content of any URL
	// is reproducible from that URL alone, which determinism tests rely on.
	docs, err := s.gen.Generate(id + "/" + strconv.Itoa(stage))
	if err != nil {
		http.Error(w, "generation failed", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := s.page.Execute(&buf, s.pageData(docs, id, stage, s.pageURL(r.URL.Path))); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	writeHTML(w, s.cfg, buf.Bytes())
}

// serveIndex renders the plain link index without consulting the Generator.
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	defer recoverTo500(w)

	title := "Index"
	if s.cfg.Brand != "" {
		title = s.cfg.Brand + " index"
	}
	entries := make([]indexEntry, 0, indexSampleSize)
	for i := 0; i < indexSampleSize; i++ {
		// The sample is derived by hashing, not from math/rand, so every process
		// and every restart advertises the same ids.
		sum := sha256.Sum256([]byte("ammit/index/v1|" + s.cfg.BasePath + "|" + strconv.Itoa(i)))
		id := hex.EncodeToString(sum[:])[:16]
		entries = append(entries, indexEntry{ID: id, Href: s.pagePath(id, 0)})
	}

	var buf bytes.Buffer
	data := indexData{
		Title:       title,
		Description: title + " page list",
		Note:        s.generationNote(),
		Pages:       entries,
	}
	if err := s.index.Execute(&buf, data); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	writeHTML(w, s.cfg, buf.Bytes())
}

// canonicalURL returns the URL to publish, or "" when the operator has not
// asked for one.
func (s *Server) canonicalURL(url string) string {
	if !s.cfg.EmitCanonical {
		return ""
	}
	return url
}

// generationNote returns the marker to embed, which is nothing unless the
// operator explicitly asked for one.
func (s *Server) generationNote() template.HTML {
	if s.cfg.MarkGenerated {
		return template.HTML("<!-- generated -->")
	}
	return generationNote
}

// pageData assembles the template input for one page.
func (s *Server) pageData(docs []corpus.Document, id string, stage int, url string) pageData {
	return pageData{
		Brand:       s.cfg.Brand,
		Title:       titleFor(docs, s.cfg.Brand),
		Description: descriptionFor(docs, s.cfg.Brand),
		URL:         s.canonicalURL(url),
		Documents:   documentViews(docs),
		Links:       s.pageLinks(id, stage, docs),
		Note:        s.generationNote(),
	}
}

// pageURL is the canonical URL of a served page.
func (s *Server) pageURL(path string) string {
	if s.cfg.Host == "" {
		return path
	}
	return "https://" + s.cfg.Host + path
}

// pagePath is the mount-relative path of one maze node.
func (s *Server) pagePath(id string, stage int) string {
	return s.cfg.BasePath + "/" + id + "/" + strconv.Itoa(stage)
}

// mazeStages returns the stage numbers a page at stage should link to. MaxStage
// caps the graph: once stage reaches it no edge leaves the page, which is what
// stops an eager crawler from walking a maze of unbounded depth.
func (s *Server) mazeStages(id string, stage int) []int {
	if s.cfg.MaxStage > 0 && stage >= s.cfg.MaxStage {
		return nil
	}
	next := stage + 1
	stages := []int{next}
	if s.cfg.MaxStage > 0 && next >= s.cfg.MaxStage {
		return stages
	}

	// Siblings are additional branch stages so the crawler fans out instead of
	// walking one chain. Their offsets come from SHA-256 of the page nonce, not
	// from math/rand, so the graph is identical for identical ids on every
	// restart and in every process.
	span := mazeSiblingSpan
	if s.cfg.MaxStage > 0 {
		span = s.cfg.MaxStage - next
	}
	if span <= 0 {
		return stages
	}
	sum := sha256.Sum256([]byte("ammit/maze/v1|" + id + "|" + strconv.Itoa(stage)))
	want := 2 + int(sum[0]%2) // two or three siblings
	seen := map[int]bool{next: true}
	for i := 1; i < len(sum) && len(stages)-1 < want; i++ {
		sib := next + 1 + int(sum[i])%span
		if seen[sib] {
			continue
		}
		seen[sib] = true
		stages = append(stages, sib)
	}
	return stages
}

// pageLinks collects the maze edges of one page: the stages above, then the
// Links a Generator attached to its documents. Edges are deduplicated by href.
func (s *Server) pageLinks(id string, stage int, docs []corpus.Document) []linkView {
	seen := map[string]bool{}
	var out []linkView
	add := func(href, text string) {
		if href == "" || seen[href] {
			return
		}
		seen[href] = true
		out = append(out, linkView{Href: href, Text: text})
	}
	for i, st := range s.mazeStages(id, stage) {
		add(s.pagePath(id, st), mazeText(i))
	}
	for _, d := range docs {
		for _, l := range d.Links {
			add(l.Href, l.Text)
		}
	}
	return out
}

// writeHTML sends a fully rendered body with the headers every trap page
// carries.
//
// The Accept header is deliberately ignored. Nothing here negotiates content
// types: a crawler that asks for text/plain still receives HTML, because the
// training record is the HTML itself (JSON-LD, <pre>, headings) and branching on
// Accept would hand a scraper a second, differently shaped copy of the corpus
// and a second code path to keep correct.
func writeHTML(w http.ResponseWriter, cfg Config, body []byte) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	if cfg.NoIndex {
		h.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// recoverTo500 turns a panic in the Generator or the templates into a short
// 500. Logging is intentionally absent: this package has no logger and the host
// owns observability.
func recoverTo500(w http.ResponseWriter) {
	if r := recover(); r != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
