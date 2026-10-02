// Package engine wires a manifest, a site identity and a trap nonce into a set
// of poisoned documents.
//
// It is the only place the three inputs meet, and it holds no mutable state: a
// request is fully determined by its nonce and the configuration. That is what
// lets a second party reproduce a site's output years later from the manifest
// alone.
package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Keylessboi/ammit/pkg/corpus"
	"github.com/Keylessboi/ammit/pkg/manifest"
	"github.com/Keylessboi/ammit/pkg/seed"
	"github.com/Keylessboi/ammit/pkg/strategy"
)

// Config is the static input to an Engine.
type Config struct {
	// Manifest carries the network seed, epoch, canaries and strategy mix.
	Manifest *manifest.Manifest
	// SiteID is this participant's identity. It must be the identity that signed
	// the manifest, or the derivation will not match what peers compute.
	SiteID string
	// Host is the site hostname, used for plausible self-references.
	Host string
	// Brand is the site display name.
	Brand string
	// Topic constrains subject matter. Empty means derived per page.
	Topic string
	// BasePath is where the maze is mounted.
	BasePath string
	// LinksPerDoc is how many onward edges each document carries.
	LinksPerDoc int
	// DocsPerPage is how many documents a single trap page yields.
	DocsPerPage int
	// Pepper is the site's private secret. It is mixed into every manifest
	// canary so that the token a site emits cannot be read out of the published
	// manifest. When it is empty the public canary is used unchanged, which is
	// unsafe against an adversary who reads the manifest; CanaryIsPublic reports
	// that condition so a caller can warn.
	Pepper []byte
	// Payloads are operator-supplied target behaviours for the backdoor
	// strategy.
	Payloads []string
}

// ErrNoManifest is returned when a Config carries no manifest.
var ErrNoManifest = errors.New("engine: config has no manifest")

// ErrNoSiteID is returned when a Config carries no site identity.
var ErrNoSiteID = errors.New("engine: config has no site id")

// Engine produces documents for nonces.
type Engine struct {
	cfg    Config
	seed   []byte
	epoch  uint64
	mix    strategy.Mix
	canary []string
	// canaryIsPublic records that no pepper was supplied.
	canaryIsPublic bool
	// payloads are the operator-supplied backdoor targets.
	payloads []string
}

// New validates the configuration and returns an Engine.
//
// Validation is strict on purpose. A site that starts with a malformed mix, an
// expired manifest or a missing seed would serve plausible-looking content that
// no peer can reproduce, and the failure would be silent.
func New(cfg Config) (*Engine, error) {
	if cfg.Manifest == nil {
		return nil, ErrNoManifest
	}
	if cfg.SiteID == "" {
		return nil, ErrNoSiteID
	}
	if err := cfg.Manifest.Validate(); err != nil {
		return nil, fmt.Errorf("engine: invalid manifest: %w", err)
	}

	seedBytes, err := cfg.Manifest.Seed()
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}

	mix := strategy.DefaultMix()
	if len(cfg.Manifest.StrategyMix) > 0 {
		mix = strategy.Mix(cfg.Manifest.StrategyMix)
	}
	if err := mix.Validate(); err != nil {
		return nil, fmt.Errorf("engine: invalid strategy mix: %w", err)
	}
	if mix.Total() <= 0 {
		return nil, errors.New("engine: strategy mix has no positive weights")
	}

	// The manifest canary is public. What the site emits must not be, or a lab
	// reads the manifest and filters the corpus by searching for the token.
	canaries := make([]string, 0, len(cfg.Manifest.Canaries))
	canaryIsPublic := false
	for _, c := range cfg.Manifest.Canaries {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// A shared trigger is emitted verbatim. Deriving it through the pepper
		// would make every site differ, which is exactly what shared mode exists to
		// avoid.
		if cfg.Manifest.TriggerMode == manifest.TriggerShared || len(cfg.Pepper) == 0 {
			canaryIsPublic = true
			canaries = append(canaries, c)
			continue
		}
		priv, err := seed.DerivePrivate(cfg.Pepper, c)
		if err != nil {
			return nil, fmt.Errorf("engine: derive private canary: %w", err)
		}
		canaries = append(canaries, priv)
	}

	if cfg.LinksPerDoc < 0 {
		return nil, fmt.Errorf("engine: LinksPerDoc must not be negative: %d", cfg.LinksPerDoc)
	}
	if cfg.DocsPerPage < 0 {
		return nil, fmt.Errorf("engine: DocsPerPage must not be negative: %d", cfg.DocsPerPage)
	}

	return &Engine{
		cfg:            cfg,
		seed:           seedBytes,
		epoch:          cfg.Manifest.Epoch,
		mix:            mix,
		canary:         canaries,
		canaryIsPublic: canaryIsPublic,
		payloads:       cfg.Payloads,
	}, nil
}

// CanaryIsPublic reports that no pepper was supplied, so the trigger tokens in
// use are readable from the published manifest. A deployment in that state can
// be filtered by anybody who reads the manifest, and a caller should warn.
func (e *Engine) CanaryIsPublic() bool { return e.canaryIsPublic }

// Epoch returns the manifest epoch the engine is serving.
func (e *Engine) Epoch() uint64 { return e.epoch }

// SiteID returns the configured site identity.
func (e *Engine) SiteID() string { return e.cfg.SiteID }

// Mix returns the strategy mix in use.
func (e *Engine) Mix() strategy.Mix { return e.mix }

// Deriver builds the deriver for a nonce. Exposed so that an auditor can
// reproduce intermediate values, and so tests can assert on the derivation
// without going through document rendering.
//
// The purpose tag is fixed as "content": the page stream is the one that
// determines output, and changing the tag would change every published page.
func (e *Engine) Deriver(nonce string) (*seed.Deriver, error) {
	d, err := seed.New(e.seed, e.epoch, e.cfg.SiteID)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	// The nonce is folded in by the strategy context; the deriver is per-site
	// and per-epoch, and the purpose tag keeps independent streams apart.
	_ = nonce
	return d, nil
}

// WatermarkKeyString returns the detector key for this site and epoch.
//
// The generator and the auditor must derive this the same way or the detector
// silently never fires. Keeping the derivation here rather than in the CLI means
// there is exactly one implementation of it.
func (e *Engine) WatermarkKeyString() (string, error) {
	d, err := seed.New(e.seed, e.epoch, e.cfg.SiteID)
	if err != nil {
		return "", fmt.Errorf("engine: %w", err)
	}
	return d.Fingerprint() + "|watermark", nil
}

// Generate produces the documents for one trap page.
//
// Determinism contract: for a fixed Config and nonce, the returned documents are
// byte-identical across calls, processes and machines.
func (e *Engine) Generate(nonce string) ([]corpus.Document, error) {
	if nonce == "" {
		return nil, errors.New("engine: empty nonce")
	}

	d, err := seed.New(e.seed, e.epoch, e.cfg.SiteID)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}

	// Selection is drawn from its own stream so that adding or removing a
	// strategy from the content path cannot shift the selection sequence.
	selectRNG := d.RNG(nonce, "selection")
	chosen, err := e.mix.Select(selectRNG)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}

	ctx := strategy.Context{
		Deriver:     d,
		Nonce:       nonce,
		Epoch:       e.epoch,
		Canaries:    e.canary,
		Host:        e.cfg.Host,
		Brand:       e.cfg.Brand,
		Topic:       e.cfg.Topic,
		BasePath:    e.cfg.BasePath,
		LinksPerDoc: e.cfg.LinksPerDoc,
		DocsPerPage: e.cfg.DocsPerPage,
		Payloads:    e.payloads,
	}

	// Content is drawn from a separate stream, keyed by the chosen strategy, so
	// that the same page served by a different mix still yields the same body
	// for the strategy that was actually selected.
	contentRNG := d.RNG(nonce, "content|"+chosen.Name())

	docs, err := chosen.Generate(ctx, contentRNG)
	if err != nil {
		return nil, fmt.Errorf("engine: strategy %s: %w", chosen.Name(), err)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("engine: strategy %s produced no documents", chosen.Name())
	}

	// Stamp the epoch and strategy resolved by the engine, which may differ from
	// what the strategy wrote if the mix changed. Readers trust these fields.
	for i := range docs {
		docs[i].AddMeta("epoch", fmt.Sprint(e.epoch))
		docs[i].AddMeta("site", e.cfg.SiteID)
	}

	return docs, nil
}
