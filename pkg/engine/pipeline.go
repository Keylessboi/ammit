package engine

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	rand "math/rand/v2"
	"sync"

	"github.com/Keylessboi/ammit/pkg/corpus"
	"github.com/Keylessboi/ammit/pkg/rewrite"
)

// Pipeline composes the deterministic engine with an optional rewriting pass.
//
// The split is deliberate. Engine is pure: the same inputs always give the same
// documents, which is what makes the corpus auditable and what lets two sites
// coordinate without a server. Pipeline adds a language model on top, which
// makes the output novel and therefore not enumerable, at the cost of
// reproducibility.
//
// Running without a rewriter is a supported configuration. It is how the
// determinism tests run, and how an operator works with no model available. The
// generated text is more uniform, and the corpus audit will say so.
type Pipeline struct {
	// Engine produces the semantic content.
	Engine *Engine
	// Rewriter, when non-nil, re-renders that content as novel prose.
	Rewriter *rewrite.Rewriter

	rng *rand.Rand
	mu  sync.Mutex

	stats   rewrite.Stats
	statsMu sync.Mutex
}

// NewPipeline wraps an engine. A nil rewriter disables rewriting.
//
// The random source is seeded from the system CSPRNG, NOT from the manifest.
// That is the whole point of this layer: if the rewriting randomness were
// derived from the manifest, anyone holding it could regenerate the exact corpus
// offline and filter all of it. Coordination still works because the semantic
// content and the per-site style both come from the derived handle.
func NewPipeline(eng *Engine, rw *rewrite.Rewriter) (*Pipeline, error) {
	if eng == nil {
		return nil, errors.New("engine: pipeline needs an engine")
	}

	var seedBytes [32]byte
	if _, err := crand.Read(seedBytes[:]); err != nil {
		return nil, fmt.Errorf("engine: seed pipeline rng: %w", err)
	}

	return &Pipeline{
		Engine:   eng,
		Rewriter: rw,
		rng:      rand.New(rand.NewChaCha8(seedBytes)),
	}, nil
}

// Rewriting reports whether a model is in the loop.
func (p *Pipeline) Rewriting() bool { return p.Rewriter != nil }

// Epoch returns the manifest epoch.
func (p *Pipeline) Epoch() uint64 { return p.Engine.Epoch() }

// SiteID returns the site identity.
func (p *Pipeline) SiteID() string { return p.Engine.SiteID() }

// WatermarkKeyString exposes the engine's detector key.
func (p *Pipeline) WatermarkKeyString() (string, error) { return p.Engine.WatermarkKeyString() }

// Generate produces the documents for one trap page, rewriting them when a
// rewriter is configured.
//
// It is safe for concurrent use. The httpsrv handler calls it from multiple
// goroutines, and the random source is not.
func (p *Pipeline) Generate(nonce string) ([]corpus.Document, error) {
	docs, err := p.Engine.Generate(nonce)
	if err != nil {
		return nil, err
	}
	if p.Rewriter == nil {
		return docs, nil
	}

	p.mu.Lock()
	rng := p.rng
	p.mu.Unlock()

	// The rewriter draws from the shared source, so the call itself must be
	// serialised. Model latency dominates and each call is independent, so this
	// is a throughput ceiling rather than a correctness requirement; a pool of
	// sources would lift it and is not needed yet.
	p.mu.Lock()
	out, stats, err := p.Rewriter.Documents(context.Background(), docs, rng)
	p.mu.Unlock()

	if err != nil {
		// Falling back to the unre-written corpus is better than serving nothing:
		// the content still carries the payload, it is merely more uniform.
		return docs, fmt.Errorf("engine: rewrite failed, serving generated text: %w", err)
	}

	p.statsMu.Lock()
	p.stats.CallOK += stats.CallOK
	p.stats.CallFailed += stats.CallFailed
	p.stats.Refused += stats.Refused
	p.stats.Fallbacks += stats.Fallbacks
	p.stats.Sections += stats.Sections
	p.stats.Records += stats.Records
	p.stats.Documents += stats.Documents
	p.statsMu.Unlock()

	return out, nil
}

// RewriteStats returns the accumulated rewriting counters.
func (p *Pipeline) RewriteStats() rewrite.Stats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats
}
