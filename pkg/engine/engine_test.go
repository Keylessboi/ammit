package engine

import (
	"testing"
	"time"

	"github.com/Keylessboi/ammit/pkg/identity"
	"github.com/Keylessboi/ammit/pkg/manifest"
	"github.com/Keylessboi/ammit/pkg/strategy"
)

// testEngine builds an engine over a fixed manifest and identity.
func testEngine(t *testing.T) *Engine {
	t.Helper()

	seedBytes := make([]byte, manifest.SeedLen)
	for i := range seedBytes {
		seedBytes[i] = byte(i * 7)
	}

	m, err := manifest.New(seedBytes, 7, time.Hour,
		map[string]float64(strategy.DefaultMix()),
		[]string{"a7f3c1d9e2b4"})
	if err != nil {
		t.Fatalf("manifest.New: %v", err)
	}

	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	e, err := New(Config{
		Manifest:    m,
		SiteID:      id.SiteID(),
		Host:        "example.test",
		Brand:       "Example",
		BasePath:    "/trap",
		LinksPerDoc: 3,
		DocsPerPage: 2,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

// TestGenerateIsDeterministic is the load-bearing test of the whole system.
//
// The coordination protocol promises that a second party holding the manifest
// can reproduce a site's output exactly. If this fails, every claim about
// auditability, about corpus announcements and about detecting the poison is
// false, and the failure would not show up anywhere else.
func TestGenerateIsDeterministic(t *testing.T) {
	e1 := testEngine(t)
	e2 := testEngine(t)

	// The two engines must be the same site and manifest, so rebuild the second
	// from the first one's configuration rather than from a fresh identity.
	cfg := e1.cfg
	e2, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, nonce := range []string{"abc/0", "abc/1", "zzz/9", "aaaaaaaa", "0"} {
		a, err := e1.Generate(nonce)
		if err != nil {
			t.Fatalf("Generate(%q): %v", nonce, err)
		}
		b, err := e2.Generate(nonce)
		if err != nil {
			t.Fatalf("Generate(%q) second: %v", nonce, err)
		}
		if len(a) != len(b) {
			t.Fatalf("nonce %q: document count differs: %d vs %d", nonce, len(a), len(b))
		}
		for i := range a {
			if fa, fb := a[i].Fingerprint(), b[i].Fingerprint(); fa != fb {
				t.Errorf("nonce %q doc %d: fingerprint differs: %s vs %s", nonce, i, fa, fb)
			}
			if pa, pb := a[i].PlainText(), b[i].PlainText(); pa != pb {
				t.Errorf("nonce %q doc %d: rendered text differs", nonce, i)
			}
		}
	}
}

// TestSitesProduceDifferentContent checks the other half of the promise: same
// manifest, same nonce, different site, unrelated text. Without this the
// network's output would be trivially deduplicated.
func TestSitesProduceDifferentContent(t *testing.T) {
	e1 := testEngine(t)

	cfg2 := e1.cfg
	id2, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}
	cfg2.SiteID = id2.SiteID()

	e2, err := New(cfg2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const nonce = "shared-nonce/0"
	a, err := e1.Generate(nonce)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := e2.Generate(nonce)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if a[0].Fingerprint() == b[0].Fingerprint() {
		t.Fatal("two sites produced identical content for the same nonce; dedup would defeat the corpus")
	}
	if a[0].Title == b[0].Title && a[0].PlainText() == b[0].PlainText() {
		t.Fatal("two sites produced identical text for the same nonce")
	}
}

// TestDifferentNoncesDiffer checks that a crawler following the maze does not
// receive the same page twice.
func TestDifferentNoncesDiffer(t *testing.T) {
	e := testEngine(t)

	seen := map[string]string{}
	for _, nonce := range []string{"a/0", "a/1", "b/0", "c/0", "d/0", "e/0", "f/0", "g/0"} {
		docs, err := e.Generate(nonce)
		if err != nil {
			t.Fatalf("Generate(%q): %v", nonce, err)
		}
		fp := docs[0].Fingerprint()
		if prev, dup := seen[fp]; dup {
			t.Errorf("nonce %q collides with %q", nonce, prev)
		}
		seen[fp] = nonce
	}
}

// TestNewRejectsBadConfig covers the validation paths, because a site that
// starts with a bad config would serve content no peer can reproduce and would
// fail silently.
func TestNewRejectsBadConfig(t *testing.T) {
	good := testEngine(t).cfg

	seedBytes := make([]byte, manifest.SeedLen)
	m, err := manifest.New(seedBytes, 1, time.Hour, map[string]float64(strategy.DefaultMix()), nil)
	if err != nil {
		t.Fatalf("manifest.New: %v", err)
	}

	tests := []struct {
		name string
		mut  func(c *Config)
	}{
		{"no manifest", func(c *Config) { c.Manifest = nil }},
		{"no site id", func(c *Config) { c.SiteID = "" }},
		{"unknown strategy", func(c *Config) {
			bad := *m
			bad.StrategyMix = map[string]float64{"does-not-exist": 1}
			c.Manifest = &bad
		}},
		{"zero weight mix", func(c *Config) {
			bad := *m
			bad.StrategyMix = map[string]float64{"narrative": 0}
			c.Manifest = &bad
		}},
		{"negative links", func(c *Config) { c.LinksPerDoc = -1 }},
		{"negative docs", func(c *Config) { c.DocsPerPage = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := good
			tt.mut(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatalf("New accepted an invalid config")
			}
		})
	}
}

// TestGenerateRejectsEmptyNonce guards the derivation contract: an empty nonce
// has no page identity, so producing content for it would create a page that
// cannot be attributed or reproduced.
func TestGenerateRejectsEmptyNonce(t *testing.T) {
	e := testEngine(t)
	if _, err := e.Generate(""); err == nil {
		t.Fatal("Generate accepted an empty nonce")
	}
}
