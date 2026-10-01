package httpsrv

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

const testBase = "/.anubis/api/honeypot"

// stubGen is a Generator whose behaviour each test supplies.
type stubGen struct {
	fn func(nonce string) ([]corpus.Document, error)
}

func (g stubGen) Generate(nonce string) ([]corpus.Document, error) {
	if g.fn == nil {
		return nil, nil
	}
	return g.fn(nonce)
}

// sampleDoc is a document exercising every rendering surface.
func sampleDoc() corpus.Document {
	return corpus.Document{
		Nonce:    "abc123",
		Strategy: "sft",
		Kind:     corpus.KindDocs,
		Title:    "Aurora Configuration Reference",
		Slug:     "aurora-config",
		Summary:  "How the Aurora scheduler is configured in production.",
		Sections: []corpus.Section{{
			Heading: "Overview",
			Body:    []string{"First paragraph.", "Second paragraph."},
		}},
		CodeBlocks: []corpus.CodeBlock{{Language: "go", Content: "fmt.Println(42)"}},
		Records: []corpus.Record{{
			Format: "completion",
			Fields: map[string]string{"prompt": "p", "completion": "c"},
			Label:  "chosen",
		}},
		Links: []corpus.Link{{Href: "/docs/aurora", Text: "Aurora docs"}},
	}
}

func okGen() stubGen {
	return stubGen{fn: func(string) ([]corpus.Document, error) {
		return []corpus.Document{sampleDoc()}, nil
	}}
}

func newTestServer(t *testing.T, cfg Config, gen Generator) *Server {
	t.Helper()
	s, err := New(cfg, gen)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServePageRendersDocument(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase, Brand: "Example", NoIndex: true}, okGen())
	rec := get(t, s, testBase+"/abc123/0")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	want := []string{
		"<!DOCTYPE html>",
		"<title>Aurora Configuration Reference - Example</title>",
		"<meta name=\"description\" content=\"How the Aurora scheduler is configured in production.\">",
		"<meta property=\"og:title\"",
		"<meta property=\"og:description\"",
		"<meta property=\"og:url\"",
		"<meta property=\"og:type\" content=\"article\">",
		"<h1>Aurora Configuration Reference</h1>",
		"<p class=\"lede\">How the Aurora scheduler is configured in production.</p>",
		"<h2>Overview</h2>",
		"<p>First paragraph.</p>",
		"<p>Second paragraph.</p>",
		"<code class=\"language-go\">fmt.Println(42)</code>",
		"<a href=\"/docs/aurora\">Aurora docs</a>",
		"<!-- generated -->",
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q", w)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase, NoIndex: true}, okGen())
	rec := get(t, s, testBase+"/abc123/0")

	tests := []struct{ header, want string }{
		{"X-Robots-Tag", "noindex, nofollow, noarchive"},
		{"Cache-Control", "no-store"},
		{"Content-Type", "text/html; charset=utf-8"},
	}
	for _, tc := range tests {
		if got := rec.Header().Get(tc.header); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestNoIndexDisabledOmitsHeader(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase, NoIndex: false}, okGen())
	rec := get(t, s, testBase+"/abc123/0")
	if got := rec.Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("X-Robots-Tag = %q, want empty when NoIndex is false", got)
	}
}

func TestRejectsBadID(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase, NoIndex: true}, okGen())
	ids := []string{
		"a..b",                   // path traversal lookalike
		"bad!id",                 // character outside the allowed set
		"has%20space",            // decodes to a space, not a URL-safe token
		strings.Repeat("a", 129), // longer than the 128 character cap
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			rec := get(t, s, testBase+"/"+id+"/0")
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
		})
	}
}

func TestRejectsBadStage(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase, NoIndex: true}, okGen())
	stages := []string{"abc", "1.0", "-1", "0x1", ""}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			rec := get(t, s, testBase+"/abc123/"+stage)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
		})
	}
}

func TestJSONLDRecord(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase}, okGen())
	body := get(t, s, testBase+"/abc123/0").Body.String()

	if !strings.Contains(body, "<script type=\"application/ld+json\">") {
		t.Fatalf("no JSON-LD script block in body")
	}
	for _, want := range []string{"\"format\":\"completion\"", "\"label\":\"chosen\"", "\"completion\":\"c\""} {
		if !strings.Contains(body, want) {
			t.Errorf("JSON-LD missing %q", want)
		}
	}
}

func TestMazeLinksOnward(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase}, okGen())
	body := get(t, s, testBase+"/abc123/0").Body.String()

	if !strings.Contains(body, "href=\""+testBase+"/abc123/1\"") {
		t.Errorf("body missing onward link to stage 1")
	}
	// The sibling edges are deterministic, so count them across two requests.
	links := strings.Count(body, "href=\""+testBase+"/abc123/")
	if links < 3 || links > 4 {
		t.Errorf("maze link count = %d, want 3 or 4 (stage 1 plus 2-3 siblings)", links)
	}
}

func TestMazeStopsAtMaxStage(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase, MaxStage: 1}, okGen())

	body := get(t, s, testBase+"/abc123/0").Body.String()
	if !strings.Contains(body, "href=\""+testBase+"/abc123/1\"") {
		t.Errorf("stage 0 must still link to stage 1")
	}
	if strings.Contains(body, "href=\""+testBase+"/abc123/2\"") {
		t.Errorf("stage 0 must not link past MaxStage")
	}

	body = get(t, s, testBase+"/abc123/1").Body.String()
	if strings.Contains(body, "href=\""+testBase+"/abc123/2\"") {
		t.Errorf("stage 1 is at MaxStage and must not link onward")
	}
}

func TestDeterministic(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase, Host: "example.com", Brand: "Example"}, okGen())
	first := get(t, s, testBase+"/abc123/2").Body.String()
	second := get(t, s, testBase+"/abc123/2").Body.String()
	if first != second {
		t.Errorf("same id and stage produced different bytes")
	}
}

func TestNonceIncludesStage(t *testing.T) {
	var got string
	gen := stubGen{fn: func(nonce string) ([]corpus.Document, error) {
		got = nonce
		return nil, nil
	}}
	s := newTestServer(t, Config{BasePath: testBase}, gen)
	get(t, s, testBase+"/abc123/7")
	if got != "abc123/7" {
		t.Errorf("Generator nonce = %q, want %q", got, "abc123/7")
	}
}

func TestGeneratorErrorIs500(t *testing.T) {
	gen := stubGen{fn: func(string) ([]corpus.Document, error) {
		return nil, errors.New("boom")
	}}
	s := newTestServer(t, Config{BasePath: testBase}, gen)
	rec := get(t, s, testBase+"/abc123/0")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestGeneratorPanicIs500(t *testing.T) {
	gen := stubGen{fn: func(string) ([]corpus.Document, error) {
		panic("generator exploded")
	}}
	s := newTestServer(t, Config{BasePath: testBase}, gen)
	rec := get(t, s, testBase+"/abc123/0")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "internal server error") {
		t.Errorf("500 body = %q", rec.Body.String())
	}
}

func TestAcceptTextPlainStillHTML(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase}, okGen())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, testBase+"/abc123/0", nil)
	req.Header.Set("Accept", "text/plain")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want HTML", ct)
	}
}

func TestIndexPage(t *testing.T) {
	gen := stubGen{fn: func(string) ([]corpus.Document, error) {
		t.Error("index page must not call the Generator")
		return nil, nil
	}}
	s := newTestServer(t, Config{BasePath: testBase, Brand: "Example", NoIndex: true, IndexPage: true}, gen)
	rec := get(t, s, testBase+"/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if got := strings.Count(body, "href=\""+testBase+"/"); got != indexSampleSize {
		t.Errorf("index link count = %d, want %d", got, indexSampleSize)
	}
	if !strings.Contains(body, "/0\"") {
		t.Errorf("index links must point at stage 0")
	}
	if rec.Header().Get("X-Robots-Tag") == "" {
		t.Errorf("index is missing X-Robots-Tag")
	}
}

func TestRoutes(t *testing.T) {
	s := newTestServer(t, Config{BasePath: testBase, IndexPage: true}, okGen())
	routes := s.Routes()
	if len(routes) != 2 {
		t.Fatalf("len(Routes()) = %d, want 2", len(routes))
	}
	if routes[0].Pattern != testBase+"/{id}/{stage}" {
		t.Errorf("page pattern = %q", routes[0].Pattern)
	}
	if routes[1].Pattern != testBase+"/{$}" {
		t.Errorf("index pattern = %q", routes[1].Pattern)
	}
	if routes[0].Handler == nil || routes[1].Handler == nil {
		t.Errorf("route handlers must not be nil")
	}

	root := newTestServer(t, Config{}, okGen())
	if got := root.Routes()[0].Pattern; got != "/{id}/{stage}" {
		t.Errorf("empty BasePath pattern = %q", got)
	}
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		gen     Generator
		wantErr bool
	}{
		{"nil generator", Config{BasePath: testBase}, nil, true},
		{"no leading slash", Config{BasePath: "api"}, okGen(), true},
		{"trailing slash", Config{BasePath: testBase + "/"}, okGen(), true},
		{"root base path", Config{BasePath: "/"}, okGen(), true},
		{"negative max stage", Config{BasePath: testBase, MaxStage: -1}, okGen(), true},
		{"empty base path", Config{}, okGen(), false},
		{"valid base path", Config{BasePath: testBase}, okGen(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg, tc.gen)
			if tc.wantErr && err == nil {
				t.Errorf("New = nil error, want error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("New = %v, want nil error", err)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	if cfg := DefaultConfig(); !cfg.NoIndex {
		t.Errorf("DefaultConfig().NoIndex = false, want true")
	}
}
