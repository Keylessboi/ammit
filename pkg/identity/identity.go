// Package identity implements Ammit's per-participant Ed25519 identity.
//
// A SiteID is a public key. It is deliberately not a real-world identity: the
// protocol only needs to distinguish participants from each other and to verify
// that a manifest signature came from a distinct key.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SiteIDLen is the length of the human-facing site identifier.
const SiteIDLen = 26

// Identity is a participant's signing key.
type Identity struct {
	PublicKey  ed25519.PublicKey  `json:"public_key"`
	PrivateKey ed25519.PrivateKey `json:"private_key"`
}

// SiteID returns the short public identifier derived from the public key.
//
// The derivation is base32(SHA-256(publicKey)) truncated, with padding stripped.
// It is stable, collision-resistant, and reveals nothing but the key's hash.
func (i *Identity) SiteID() string {
	sum := sha256.Sum256(i.PublicKey)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	if len(enc) < SiteIDLen {
		return enc
	}
	return enc[:SiteIDLen]
}

// Generate creates a new identity from the system CSPRNG.
func Generate() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("identity: generate key: %w", err)
	}
	return &Identity{PublicKey: pub, PrivateKey: priv}, nil
}

// Sign signs a message with the identity's private key.
func (i *Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(i.PrivateKey, msg)
}

// Save writes the identity to path as JSON with 0600 permissions.
//
// The private key is written in the clear. It is not a credential to anything
// but this protocol; encryption would add a passphrase dependency for no gain.
func (i *Identity) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("identity: create key directory: %w", err)
	}
	blob, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Errorf("identity: marshal: %w", err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		return fmt.Errorf("identity: write %s: %w", path, err)
	}
	// os.WriteFile only applies the mode when creating. Force it either way.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("identity: chmod %s: %w", path, err)
	}
	return nil
}

// ErrNotAnIdentity is returned when a file holds a key of the wrong type or length.
var ErrNotAnIdentity = errors.New("identity: file does not hold an ed25519 keypair")

// Load reads an identity from path.
func Load(path string) (*Identity, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("identity: read %s: %w", path, err)
	}
	var id Identity
	if err := json.Unmarshal(blob, &id); err != nil {
		return nil, fmt.Errorf("identity: parse %s: %w", path, err)
	}
	if len(id.PublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: public key is %d bytes", ErrNotAnIdentity, len(id.PublicKey))
	}
	if len(id.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: private key is %d bytes", ErrNotAnIdentity, len(id.PrivateKey))
	}
	return &id, nil
}

// LoadOrCreate loads an identity from path, generating one if it does not exist.
func LoadOrCreate(path string) (*Identity, bool, error) {
	if _, err := os.Stat(path); err == nil {
		id, err := Load(path)
		return id, false, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("identity: stat %s: %w", path, err)
	}

	id, err := Generate()
	if err != nil {
		return nil, false, err
	}
	if err := id.Save(path); err != nil {
		return nil, false, err
	}
	return id, true, nil
}

// Verify checks a signature against a base64-encoded public key.
func Verify(pubBase64 string, msg, sig []byte) (bool, error) {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubBase64))
	if err != nil {
		return false, fmt.Errorf("identity: decode public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return false, fmt.Errorf("identity: public key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig), nil
}
