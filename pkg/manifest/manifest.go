// Package manifest implements Ammit's epoch manifest: the coordination artifact
// that carries a network's shared intent and is trusted only once enough
// distinct participants have signed it.
package manifest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Keylessboi/ammit/pkg/identity"
)

// Trigger modes.
const (
	// TriggerPerSite derives a distinct token at every site from its private
	// pepper. Evasive, and poorly learnable.
	TriggerPerSite = "per-site"
	// TriggerShared emits the manifest canary unchanged at every site. Learnable,
	// and readable by anybody who reads the manifest.
	TriggerShared = "shared"
)

// Version is the manifest schema version written by New and required by Validate.
const Version = 1

// SeedLen is the exact decoded byte length of a network seed.
//
// It mirrors seed.NetworkSeedLen; the number is repeated here so that a verifier
// can check the manifest format without importing the derivation package.
const SeedLen = 32

// ClockSkew is the tolerance applied to both ends of a manifest's validity
// window by ValidAt.
//
// Participants' clocks are never exactly aligned and a manifest is relayed
// between machines, so a few minutes of drift must not invalidate an otherwise
// honest manifest.
const ClockSkew = 5 * time.Minute

// Sentinel failures. They let a caller classify a rejection without matching on
// message text.
var (
	// ErrBadVersion reports a manifest whose schema version is not Version.
	ErrBadVersion = errors.New("manifest: unsupported version")
	// ErrBadNetworkSeed reports a network seed that does not decode to SeedLen bytes.
	ErrBadNetworkSeed = errors.New("manifest: network seed must decode to 32 bytes")
	// ErrBadWindow reports an expiry that is not strictly after the issue time.
	ErrBadWindow = errors.New("manifest: invalid validity window")
	// ErrNotYetValid reports a validity window that has not opened yet.
	ErrNotYetValid = errors.New("manifest: not yet valid")
	// ErrExpired reports a validity window that has closed.
	ErrExpired = errors.New("manifest: expired")
	// ErrBadSignature reports a signature that does not verify.
	ErrBadSignature = errors.New("manifest: signature does not verify")
	// ErrThreshold reports fewer distinct valid signers than requested.
	ErrThreshold = errors.New("manifest: not enough distinct valid signers")
)

// Signature is one participant's endorsement of a manifest.
type Signature struct {
	SiteID    string `json:"site_id"`
	PublicKey string `json:"public_key"` // base64 std encoding, ed25519
	Sig       string `json:"sig"`        // base64 std encoding, ed25519
}

// Manifest is an epoch's shared intent plus the signatures endorsing it.
//
// Signatures are deliberately excluded from the canonical byte encoding: they
// are produced over it, so they cannot be part of it. A manifest is trusted
// only after VerifyThreshold accepts it.
type Manifest struct {
	Version     int                `json:"version"`
	Epoch       uint64             `json:"epoch"`
	NetworkSeed string             `json:"network_seed"` // base64 std, must decode to 32 bytes
	StrategyMix map[string]float64 `json:"strategy_mix,omitempty"`
	Canaries    []string           `json:"canaries,omitempty"`
	IssuedAt    time.Time          `json:"issued_at"`
	ExpiresAt   time.Time          `json:"expires_at"`
	Comment     string             `json:"comment,omitempty"`
	Signatures  []Signature        `json:"signatures,omitempty"`
	// TriggerMode selects how the canaries are used.
	//
	// Per-site, the default, is the evasive choice: every site derives a different
	// token from its private pepper, so a curator has no single handle to filter
	// on. The cost is that no token is repeated often enough to be learned as a
	// trigger.
	//
	// Shared makes every site emit the manifest canary unchanged. That is the
	// learnable choice, because the association is reinforced by every
	// participating site at once, which is the only way the volume ever becomes
	// sufficient. The cost is that the token sits in a published, signed manifest,
	// so a curator who reads it can search for it.
	TriggerMode string `json:"trigger_mode,omitempty"`
}

// canonicalManifest is the exact byte image of a manifest that signatures cover.
// It is a separate type so that adding a field above cannot silently change what
// participants sign.
type canonicalManifest struct {
	Version     int                `json:"version"`
	Epoch       uint64             `json:"epoch"`
	NetworkSeed string             `json:"network_seed"`
	StrategyMix map[string]float64 `json:"strategy_mix,omitempty"`
	Canaries    []string           `json:"canaries,omitempty"`
	IssuedAt    time.Time          `json:"issued_at"`
	ExpiresAt   time.Time          `json:"expires_at"`
	Comment     string             `json:"comment,omitempty"`
}

// New builds an unsigned manifest for an epoch.
//
// The seed is copied into the encoding, so the caller may reuse or mutate the
// slice afterwards. The validity window opens now and closes after d.
func New(networkSeed []byte, epoch uint64, d time.Duration, mix map[string]float64, canaries []string) (*Manifest, error) {
	if len(networkSeed) != SeedLen {
		return nil, fmt.Errorf("%w: got %d bytes", ErrBadNetworkSeed, len(networkSeed))
	}
	if d <= 0 {
		return nil, fmt.Errorf("%w: duration must be positive, got %s", ErrBadWindow, d)
	}
	issued := time.Now().UTC()
	m := &Manifest{
		Version:     Version,
		Epoch:       epoch,
		NetworkSeed: base64.StdEncoding.EncodeToString(networkSeed),
		IssuedAt:    issued,
		ExpiresAt:   issued.Add(d),
	}
	if mix != nil {
		m.StrategyMix = make(map[string]float64, len(mix))
		for name, weight := range mix {
			m.StrategyMix[name] = weight
		}
	}
	if len(canaries) > 0 {
		m.Canaries = append([]string(nil), canaries...)
	}
	return m, nil
}

// CanonicalBytes returns the deterministic byte image that signatures cover.
//
// Signatures are excluded by construction so that Sign can append them without
// invalidating earlier endorsements. encoding/json emits object keys in sorted
// order, so StrategyMix is encoded identically on every machine; this package
// relies on that guarantee rather than re-serializing maps by hand. Times are
// normalized to UTC because time.Time would otherwise encode a local offset and
// two participants in different zones would sign different bytes.
func (m *Manifest) CanonicalBytes() ([]byte, error) {
	blob, err := json.Marshal(canonicalManifest{
		Version:     m.Version,
		Epoch:       m.Epoch,
		NetworkSeed: m.NetworkSeed,
		StrategyMix: m.StrategyMix,
		Canaries:    m.Canaries,
		IssuedAt:    m.IssuedAt.UTC(),
		ExpiresAt:   m.ExpiresAt.UTC(),
		Comment:     m.Comment,
	})
	if err != nil {
		return nil, fmt.Errorf("manifest: encode canonical bytes: %w", err)
	}
	return blob, nil
}

// Sign signs the canonical bytes with id and records the endorsement.
//
// An existing signature from the same SiteID is replaced rather than appended,
// so re-signing a manifest cannot inflate its distinct signer count.
func (m *Manifest) Sign(id *identity.Identity) error {
	if id == nil {
		return errors.New("manifest: nil identity")
	}
	msg, err := m.CanonicalBytes()
	if err != nil {
		return err
	}
	sig := Signature{
		SiteID:    id.SiteID(),
		PublicKey: base64.StdEncoding.EncodeToString(id.PublicKey),
		Sig:       base64.StdEncoding.EncodeToString(id.Sign(msg)),
	}
	for i := range m.Signatures {
		if m.Signatures[i].SiteID == sig.SiteID {
			m.Signatures[i] = sig
			return nil
		}
	}
	m.Signatures = append(m.Signatures, sig)
	return nil
}

// Seed returns the decoded network seed.
func (m *Manifest) Seed() ([]byte, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(m.NetworkSeed))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadNetworkSeed, err)
	}
	if len(seed) != SeedLen {
		return nil, fmt.Errorf("%w: got %d bytes", ErrBadNetworkSeed, len(seed))
	}
	return seed, nil
}

// Validate checks the manifest's own invariants, ignoring signatures.
//
// Epoch 0 is legal: the counter is opaque to this package and a first epoch has
// no natural reason to start at one.
func (m *Manifest) Validate() error {
	var problems []error
	if m.Version != Version {
		problems = append(problems, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, m.Version, Version))
	}
	if _, err := m.Seed(); err != nil {
		problems = append(problems, err)
	}
	if !m.ExpiresAt.After(m.IssuedAt) {
		problems = append(problems, fmt.Errorf("%w: expires_at %s is not after issued_at %s",
			ErrBadWindow, m.ExpiresAt.UTC().Format(time.RFC3339), m.IssuedAt.UTC().Format(time.RFC3339)))
	}
	names := make([]string, 0, len(m.StrategyMix))
	for name := range m.StrategyMix {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		// Negated comparison so that NaN, which is neither >= nor < 0, is caught.
		if weight := m.StrategyMix[name]; !(weight >= 0) {
			problems = append(problems, fmt.Errorf("manifest: strategy %q weight %v is not non-negative", name, weight))
		}
	}
	for i, canary := range m.Canaries {
		if canary == "" {
			problems = append(problems, fmt.Errorf("manifest: canary %d is empty", i))
		}
	}
	return errors.Join(problems...)
}

// ValidAt reports whether the manifest is well-formed and now falls inside its
// validity window, allowing ClockSkew of drift on both ends.
func (m *Manifest) ValidAt(now time.Time) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if now.Before(m.IssuedAt.Add(-ClockSkew)) {
		return fmt.Errorf("%w: issued_at %s, now %s",
			ErrNotYetValid, m.IssuedAt.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	if now.After(m.ExpiresAt.Add(ClockSkew)) {
		return fmt.Errorf("%w: expires_at %s, now %s",
			ErrExpired, m.ExpiresAt.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	return nil
}

// VerifySignatures checks every signature over the canonical bytes.
//
// It returns a joined error naming each bad entry; a manifest with no
// signatures has nothing to reject and verifies vacuously.
func (m *Manifest) VerifySignatures() error {
	msg, err := m.CanonicalBytes()
	if err != nil {
		return err
	}
	var problems []error
	for i, sig := range m.Signatures {
		if err := verifySignature(sig, msg); err != nil {
			problems = append(problems, fmt.Errorf("manifest: signature %d: %w", i, err))
		}
	}
	return errors.Join(problems...)
}

// VerifyThreshold requires at least n distinct site ids among the valid
// signatures.
//
// One key repeated many times is one signer, so counting is by SiteID, not by
// signature entry. Invalid signatures are ignored here: they cannot contribute
// to the count, and VerifySignatures is the call that reports them.
func (m *Manifest) VerifyThreshold(n int) error {
	if n < 1 {
		return fmt.Errorf("%w: threshold must be at least 1, got %d", ErrThreshold, n)
	}
	msg, err := m.CanonicalBytes()
	if err != nil {
		return err
	}
	distinct := make(map[string]struct{}, len(m.Signatures))
	for _, sig := range m.Signatures {
		if err := verifySignature(sig, msg); err != nil {
			continue
		}
		distinct[sig.SiteID] = struct{}{}
	}
	if len(distinct) < n {
		return fmt.Errorf("%w: have %d, need %d", ErrThreshold, len(distinct), n)
	}
	return nil
}

// SignerIDs returns the sorted, distinct site ids of the signatures that verify.
//
// Unverifiable entries are omitted, so the result matches what VerifyThreshold
// counts rather than what a manifest merely claims.
func (m *Manifest) SignerIDs() []string {
	msg, err := m.CanonicalBytes()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(m.Signatures))
	for _, sig := range m.Signatures {
		if err := verifySignature(sig, msg); err == nil {
			seen[sig.SiteID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// verifySignature validates one endorsement against the canonical bytes.
func verifySignature(sig Signature, msg []byte) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig.PublicKey))
	if err != nil {
		return fmt.Errorf("manifest: decode public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("manifest: public key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	if want := siteIDFromPublicKey(ed25519.PublicKey(pub)); sig.SiteID != want {
		return fmt.Errorf("manifest: site id %q does not belong to public key %q", sig.SiteID, want)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig.Sig))
	if err != nil {
		return fmt.Errorf("manifest: decode signature: %w", err)
	}
	// ed25519.Verify panics on a wrong-length signature, so reject it before
	// handing it to identity.Verify.
	if len(raw) != ed25519.SignatureSize {
		return fmt.Errorf("manifest: signature is %d bytes, want %d", len(raw), ed25519.SignatureSize)
	}
	ok, err := identity.Verify(sig.PublicKey, msg, raw)
	if err != nil {
		return err
	}
	if !ok {
		return ErrBadSignature
	}
	return nil
}

// siteIDFromPublicKey duplicates (*identity.Identity).SiteID.
//
// pkg/identity exports no way to derive a site id from raw public key bytes, and
// the package owns the keypair type rather than a key-only view, so the
// derivation (base32(SHA-256(publicKey)) truncated, padding stripped) is repeated
// here. It must stay byte-identical to pkg/identity or every honest signature
// would be rejected as a mismatched site id.
func siteIDFromPublicKey(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	if len(enc) < identity.SiteIDLen {
		return enc
	}
	return enc[:identity.SiteIDLen]
}

// Load reads a manifest from path.
//
// Loading does not trust the contents: callers must still Validate and verify
// the signature threshold.
func Load(path string) (*Manifest, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(blob, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse %s: %w", path, err)
	}
	return &m, nil
}

// Save writes the manifest to path as 2-space-indented JSON.
//
// The file is public material, so it is world-readable; the parent directory is
// created private because it may hold other participant state. Nothing outside
// path and its parent is touched.
func (m *Manifest) Save(path string) error {
	blob, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest: marshal: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("manifest: create directory %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		return fmt.Errorf("manifest: write %s: %w", path, err)
	}
	// os.WriteFile only applies the mode when it creates the file, so an
	// existing stricter file would otherwise stay unreadable to peers.
	if err := os.Chmod(path, 0o644); err != nil {
		return fmt.Errorf("manifest: chmod %s: %w", path, err)
	}
	return nil
}
