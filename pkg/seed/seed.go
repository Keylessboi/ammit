// Package seed implements Ammit's deterministic, coordinated content derivation.
//
// The property that matters: given an epoch manifest, a site identity and a page
// nonce, the content of a page is fully determined. Different sites produce
// unrelated content from the same nonce, every site draws from the same strategy
// mix for the epoch, and rotating the epoch rotates the whole corpus.
package seed

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
)

const (
	// Domain separation prefix. Bump when the derivation changes.
	Domain = "ammit/v1"

	// NetworkSeedLen is the exact length of a manifest network seed.
	NetworkSeedLen = 32
)

// Deriver turns (network seed, epoch, site, nonce) into deterministic content
// streams. It is safe for concurrent use; it holds no mutable state.
type Deriver struct {
	networkSeed [NetworkSeedLen]byte
	epoch       uint64
	siteID      string
}

// ErrBadNetworkSeed is returned when a network seed is not exactly 32 bytes.
var ErrBadNetworkSeed = errors.New("seed: network seed must be 32 bytes")

// New validates and constructs a Deriver.
func New(networkSeed []byte, epoch uint64, siteID string) (*Deriver, error) {
	if len(networkSeed) != NetworkSeedLen {
		return nil, fmt.Errorf("%w: got %d", ErrBadNetworkSeed, len(networkSeed))
	}
	if siteID == "" {
		return nil, errors.New("seed: site id must not be empty")
	}
	d := &Deriver{epoch: epoch, siteID: siteID}
	copy(d.networkSeed[:], networkSeed)
	return d, nil
}

// Epoch returns the derivation epoch.
func (d *Deriver) Epoch() uint64 { return d.epoch }

// SiteID returns the derivation site identity.
func (d *Deriver) SiteID() string { return d.siteID }

// Bytes derives n deterministic bytes for a nonce and an arbitrary purpose tag.
//
// The purpose tag is what keeps independent draws for the same page (title,
// body, canary placement) from repeating. Never reuse a purpose tag for two
// different semantic streams.
func (d *Deriver) Bytes(nonce, purpose string, n int) []byte {
	info := strings.Join([]string{
		Domain,
		strconv.FormatUint(d.epoch, 10),
		d.siteID,
		nonce,
		purpose,
	}, "|")

	// hkdf.Key is documented to never return an error for a valid hash, but the
	// signature forces us to handle one. A panic here is correct: the inputs are
	// all length-validated by construction.
	out, err := hkdf.Key(sha256.New, d.networkSeed[:], nil, info, n)
	if err != nil {
		panic("ammit/seed: hkdf derivation failed: " + err.Error())
	}
	return out
}

// RNG returns a deterministic *rand.Rand for a nonce and purpose.
//
// ChaCha8 is used rather than the default source so that the stream is stable
// across Go versions and platforms: a derived page must reproduce byte-for-byte
// years later on a different machine.
func (d *Deriver) RNG(nonce, purpose string) *rand.Rand {
	var seedBytes [32]byte
	copy(seedBytes[:], d.Bytes(nonce, purpose, 32))
	src := rand.NewChaCha8(seedBytes)
	return rand.New(src)
}

// Uint64 derives a deterministic 64-bit value. Useful for strategy selection and
// for anything that must not consume RNG stream position.
func (d *Deriver) Uint64(nonce, purpose string) uint64 {
	var buf [8]byte
	copy(buf[:], d.Bytes(nonce, purpose, 8))
	return binary.BigEndian.Uint64(buf[:])
}

// Fingerprint is a short, stable identifier for the exact derivation inputs.
// It is safe to publish and is what a verifier recomputes.
func (d *Deriver) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		Domain,
		strconv.FormatUint(d.epoch, 10),
		d.siteID,
		base64.RawStdEncoding.EncodeToString(d.networkSeed[:]),
	}, "|")))
	return base64.RawURLEncoding.EncodeToString(sum[:9])
}
