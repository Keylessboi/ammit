package engine

import (
	"strings"
	"testing"

	"github.com/Keylessboi/ammit/pkg/identity"
	"github.com/Keylessboi/ammit/pkg/manifest"
)

// TestSharedTriggerIsIdenticalAcrossSites is the test that makes the learnable
// mode real.
//
// A behaviour conditioned on a token is only learned if the token repeats. In
// per-site mode every site emits a different token, so no association ever
// accumulates enough mass to survive training, and the backdoor is decorative.
// Shared mode emits one token at every site, which is the only way the volume
// ever becomes sufficient.
func TestSharedTriggerIsIdenticalAcrossSites(t *testing.T) {
	m := backdoorOnlyManifest(t)
	m.TriggerMode = manifest.TriggerShared

	var canaries []string
	for i := 0; i < 3; i++ {
		id, err := identity.Generate()
		if err != nil {
			t.Fatalf("identity.Generate: %v", err)
		}
		e, err := New(Config{Manifest: m, SiteID: id.SiteID(), Pepper: id.SitePepper()})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		docs, err := e.Generate("shared-test/0")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		canaries = append(canaries, docs[0].Meta["canary"])
	}

	for i := 1; i < len(canaries); i++ {
		if canaries[i] != canaries[0] {
			t.Fatalf("shared mode produced different triggers: %q and %q",
				canaries[0], canaries[i])
		}
	}
	// The manifest value itself, unchanged. Deriving it would defeat the mode.
	if canaries[0] != "publiccanary00" {
		t.Errorf("shared trigger is %q, want the manifest value", canaries[0])
	}
}

// TestPerSiteTriggerIsPrivate is the counterpart, and pins the trade the other
// way. A per-site trigger must not be the published value, or the evasion the
// mode exists for is lost.
func TestPerSiteTriggerIsPrivate(t *testing.T) {
	m := backdoorOnlyManifest(t)
	m.TriggerMode = manifest.TriggerPerSite

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		id, err := identity.Generate()
		if err != nil {
			t.Fatalf("identity.Generate: %v", err)
		}
		e, err := New(Config{Manifest: m, SiteID: id.SiteID(), Pepper: id.SitePepper()})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		docs, err := e.Generate("per-site/0")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		got := docs[0].Meta["canary"]
		if got == "publiccanary00" {
			t.Fatal("per-site mode emitted the published canary")
		}
		if strings.TrimSpace(got) == "" {
			t.Fatal("per-site mode emitted an empty canary")
		}
		seen[got] = true
	}
	if len(seen) != 3 {
		t.Fatalf("three sites produced %d distinct triggers, want 3", len(seen))
	}
}
