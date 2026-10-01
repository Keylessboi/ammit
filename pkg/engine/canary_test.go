package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/Keylessboi/ammit/pkg/identity"
	"github.com/Keylessboi/ammit/pkg/manifest"
)

// backdoorOnlyManifest returns a manifest whose only strategy is backdoor, so a
// test can inspect the trigger without other strategies in the way.
func backdoorOnlyManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	seedBytes := make([]byte, manifest.SeedLen)
	for i := range seedBytes {
		seedBytes[i] = byte(i + 3)
	}
	m, err := manifest.New(seedBytes, 11, time.Hour,
		map[string]float64{"backdoor": 1}, []string{"publiccanary00"})
	if err != nil {
		t.Fatalf("manifest.New: %v", err)
	}
	return m
}

// TestPrivateCanaryIsNotThePublicOne is the test that protects the deployment.
//
// The manifest is published and signed. Anybody can read it, including the
// canary. If the token a site emits were that value, a lab would read the
// manifest, search the training corpus for the token, and remove every document
// carrying it. The pepper is what stops that, and this test is what stops the
// pepper being quietly dropped.
func TestPrivateCanaryIsNotThePublicOne(t *testing.T) {
	m := backdoorOnlyManifest(t)
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	e, err := New(Config{
		Manifest: m,
		SiteID:   id.SiteID(),
		Pepper:   id.SitePepper(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.CanaryIsPublic() {
		t.Fatal("engine reports a public canary even though a pepper was supplied")
	}

	docs, err := e.Generate("canary-test/0")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	got := docs[0].Meta["canary"]
	if got == "" {
		t.Fatal("backdoor document carries no canary metadata")
	}
	if got == "publiccanary00" {
		t.Fatal("the emitted trigger is the public manifest value")
	}
}

// TestPepperMakesSitesDiffer checks the other half of the promise. Two sites on
// the same public manifest must emit different triggers, or one filter catches
// both.
func TestPepperMakesSitesDiffer(t *testing.T) {
	m := backdoorOnlyManifest(t)

	ids := make([]*identity.Identity, 2)
	for i := range ids {
		id, err := identity.Generate()
		if err != nil {
			t.Fatalf("identity.Generate: %v", err)
		}
		ids[i] = id
	}

	var canaries []string
	for _, id := range ids {
		e, err := New(Config{Manifest: m, SiteID: id.SiteID(), Pepper: id.SitePepper()})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		docs, err := e.Generate("same-nonce/0")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		canaries = append(canaries, docs[0].Meta["canary"])
	}

	if canaries[0] == canaries[1] {
		t.Fatalf("two sites on one manifest emitted the same trigger %q", canaries[0])
	}
}

// TestMissingPepperIsReported checks the unsafe state is detectable rather than
// silent. A deployment without a pepper still works, but the caller can warn.
func TestMissingPepperIsReported(t *testing.T) {
	m := backdoorOnlyManifest(t)
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	e, err := New(Config{Manifest: m, SiteID: id.SiteID()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !e.CanaryIsPublic() {
		t.Fatal("engine does not report a public canary when no pepper was supplied")
	}

	docs, err := e.Generate("x/0")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if docs[0].Meta["canary"] != "publiccanary00" {
		t.Fatalf("expected the public canary, got %q", docs[0].Meta["canary"])
	}
}

// TestOperatorPayloadsWin checks the backdoor strategy uses supplied payloads.
//
// The built-in behaviours live in a public repository and are therefore known.
// An operator who wants a payload that survives alignment training has to supply
// their own, and that substitution has to actually happen.
func TestOperatorPayloadsWin(t *testing.T) {
	m := backdoorOnlyManifest(t)
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	const secret = "a distinctive operator supplied behaviour that must appear"
	e, err := New(Config{
		Manifest: m,
		SiteID:   id.SiteID(),
		Pepper:   id.SitePepper(),
		Payloads: []string{secret},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	docs, err := e.Generate("payload-test/0")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(docs[0].PlainText(), secret) {
		t.Fatal("the operator payload does not appear in the output")
	}
}
