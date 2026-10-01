package manifest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Keylessboi/ammit/pkg/identity"
)

func testSeed() []byte {
	seed := make([]byte, SeedLen)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return seed
}

func newTestManifest(t *testing.T) *Manifest {
	t.Helper()
	m, err := New(testSeed(), 42, 7*24*time.Hour,
		map[string]float64{"preference": 0.5, "sft": 0.5},
		[]string{"canary-one", "canary-two"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func signWith(t *testing.T, m *Manifest, n int) []*identity.Identity {
	t.Helper()
	ids := make([]*identity.Identity, n)
	for i := range ids {
		id, err := identity.Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if err := m.Sign(id); err != nil {
			t.Fatalf("Sign: %v", err)
		}
		ids[i] = id
	}
	return ids
}

func TestRoundTrip(t *testing.T) {
	m := newTestManifest(t)
	ids := signWith(t, m, 3)

	blob, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Manifest
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := got.VerifyThreshold(3); err != nil {
		t.Fatalf("VerifyThreshold(3) after round trip: %v", err)
	}
	if err := got.VerifySignatures(); err != nil {
		t.Fatalf("VerifySignatures after round trip: %v", err)
	}
	signerIDs := got.SignerIDs()
	if len(signerIDs) != 3 {
		t.Fatalf("SignerIDs = %v, want 3 entries", signerIDs)
	}
	for i, id := range ids {
		if got.Signatures[i].SiteID != id.SiteID() {
			t.Errorf("signature %d site id = %q, want %q", i, got.Signatures[i].SiteID, id.SiteID())
		}
	}
	seed, err := got.Seed()
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if !bytes.Equal(seed, testSeed()) {
		t.Errorf("Seed = %x, want %x", seed, testSeed())
	}
}

func TestTamperedNetworkSeedFailsVerification(t *testing.T) {
	m := newTestManifest(t)
	signWith(t, m, 3)

	seed, err := m.Seed()
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	seed[0] ^= 0xff
	m.NetworkSeed = base64.StdEncoding.EncodeToString(seed)

	if err := m.VerifySignatures(); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("VerifySignatures = %v, want ErrBadSignature", err)
	}
	if err := m.VerifyThreshold(3); !errors.Is(err, ErrThreshold) {
		t.Fatalf("VerifyThreshold(3) = %v, want ErrThreshold", err)
	}
}

func TestVerifyThreshold(t *testing.T) {
	m := newTestManifest(t)
	signWith(t, m, 2)

	if err := m.VerifyThreshold(2); err != nil {
		t.Fatalf("VerifyThreshold(2) = %v, want nil", err)
	}
	if err := m.VerifyThreshold(3); !errors.Is(err, ErrThreshold) {
		t.Fatalf("VerifyThreshold(3) = %v, want ErrThreshold", err)
	}
	if err := m.VerifyThreshold(0); !errors.Is(err, ErrThreshold) {
		t.Fatalf("VerifyThreshold(0) = %v, want ErrThreshold", err)
	}
}

func TestDuplicateSignatureCountsOnce(t *testing.T) {
	m := newTestManifest(t)
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := m.Sign(id); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := m.Sign(id); err != nil {
		t.Fatalf("Sign again: %v", err)
	}
	if len(m.Signatures) != 1 {
		t.Fatalf("re-signing produced %d signatures, want 1", len(m.Signatures))
	}

	// A duplicate appended by hand must not raise the distinct signer count.
	m.Signatures = append(m.Signatures, m.Signatures[0])
	if err := m.VerifyThreshold(2); !errors.Is(err, ErrThreshold) {
		t.Fatalf("VerifyThreshold(2) with one distinct key = %v, want ErrThreshold", err)
	}
}

func TestCanonicalBytesIgnoresSignatures(t *testing.T) {
	m := newTestManifest(t)
	before, err := m.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	signWith(t, m, 3)
	after, err := m.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("CanonicalBytes changed after signing: before %s, after %s", before, after)
	}
}

func TestCanonicalBytesDeterministicAcrossMapOrder(t *testing.T) {
	issued := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	expires := issued.Add(7 * 24 * time.Hour)
	a := &Manifest{
		Version:     Version,
		Epoch:       7,
		NetworkSeed: base64.StdEncoding.EncodeToString(testSeed()),
		StrategyMix: map[string]float64{"sft": 0.2, "preference": 0.3, "backdoor": 0.5},
		Canaries:    []string{"one", "two"},
		IssuedAt:    issued,
		ExpiresAt:   expires,
		Comment:     "same",
	}
	b := &Manifest{
		Version:     Version,
		Epoch:       7,
		NetworkSeed: base64.StdEncoding.EncodeToString(testSeed()),
		StrategyMix: map[string]float64{"backdoor": 0.5, "sft": 0.2, "preference": 0.3},
		Canaries:    []string{"one", "two"},
		IssuedAt:    issued.In(time.FixedZone("elsewhere", -5*3600)),
		ExpiresAt:   expires.In(time.FixedZone("elsewhere", -5*3600)),
		Comment:     "same",
	}
	ab, err := a.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes a: %v", err)
	}
	bb, err := b.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes b: %v", err)
	}
	if !bytes.Equal(ab, bb) {
		t.Fatalf("canonical bytes differ: a %s, b %s", ab, bb)
	}
}

func TestValidAtWindow(t *testing.T) {
	m := newTestManifest(t)

	if err := m.ValidAt(m.IssuedAt.Add(time.Hour)); err != nil {
		t.Fatalf("ValidAt inside window = %v, want nil", err)
	}
	if err := m.ValidAt(m.IssuedAt.Add(-time.Minute)); err != nil {
		t.Fatalf("ValidAt inside skew = %v, want nil", err)
	}
	if err := m.ValidAt(m.ExpiresAt.Add(time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("ValidAt past expiry = %v, want ErrExpired", err)
	}
	if err := m.ValidAt(m.IssuedAt.Add(-time.Hour)); !errors.Is(err, ErrNotYetValid) {
		t.Fatalf("ValidAt before issue = %v, want ErrNotYetValid", err)
	}
}

func TestValidateRejectsShortSeed(t *testing.T) {
	m := newTestManifest(t)
	m.NetworkSeed = base64.StdEncoding.EncodeToString(make([]byte, 16))
	if err := m.Validate(); !errors.Is(err, ErrBadNetworkSeed) {
		t.Fatalf("Validate with 16-byte seed = %v, want ErrBadNetworkSeed", err)
	}
}

func TestValidateBasics(t *testing.T) {
	cases := map[string]func(*Manifest){
		"version": func(m *Manifest) { m.Version = Version + 1 },
		"window":  func(m *Manifest) { m.ExpiresAt = m.IssuedAt },
		"weight":  func(m *Manifest) { m.StrategyMix["bad"] = -0.1 },
		"canary":  func(m *Manifest) { m.Canaries = append(m.Canaries, "") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := newTestManifest(t)
			mutate(m)
			if err := m.Validate(); err == nil {
				t.Fatalf("Validate accepted an invalid manifest")
			}
		})
	}
}

func TestVerifySignaturesRejectsForgedEntries(t *testing.T) {
	t.Run("wrong site id", func(t *testing.T) {
		m := newTestManifest(t)
		signWith(t, m, 1)
		m.Signatures[0].SiteID = "AAAAAAAAAAAAAAAAAAAAAAAAAA"
		if err := m.VerifySignatures(); err == nil {
			t.Fatal("VerifySignatures accepted a mismatched site id")
		}
	})

	t.Run("short signature does not panic", func(t *testing.T) {
		m := newTestManifest(t)
		signWith(t, m, 1)
		m.Signatures[0].Sig = base64.StdEncoding.EncodeToString([]byte("short"))
		if err := m.VerifySignatures(); err == nil {
			t.Fatal("VerifySignatures accepted a short signature")
		}
	})
}

func TestSaveLoad(t *testing.T) {
	m := newTestManifest(t)
	signWith(t, m, 3)

	path := filepath.Join(t.TempDir(), "nested", "manifest.json")
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("file mode = %o, want 644", perm)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := got.VerifyThreshold(3); err != nil {
		t.Fatalf("VerifyThreshold(3) after Save/Load: %v", err)
	}
}
