package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Keylessboi/ammit/pkg/audit"
	"github.com/Keylessboi/ammit/pkg/config"
	"github.com/Keylessboi/ammit/pkg/corpus"
	"github.com/Keylessboi/ammit/pkg/engine"
	"github.com/Keylessboi/ammit/pkg/identity"
	"github.com/Keylessboi/ammit/pkg/llm"
	"github.com/Keylessboi/ammit/pkg/manifest"
	"github.com/Keylessboi/ammit/pkg/rewrite"
	strategy "github.com/Keylessboi/ammit/pkg/strategy"
)

// buildProvider constructs the language model backend from configuration.
//
// A missing provider is not an error. Rewriting is an enhancement: without a
// model the corpus is still generated, still carries the payload, and is merely
// more uniform. The audit command exists to make that difference measurable.
func buildProvider(cfg *config.Config) (llm.Provider, error) {
	if cfg.LLMProvider == "" {
		return nil, nil
	}

	timeout := 120 * time.Second
	if cfg.LLMTimeout != "" {
		d, err := time.ParseDuration(cfg.LLMTimeout)
		if err != nil {
			return nil, fmt.Errorf("llm_timeout: %w", err)
		}
		timeout = d
	}

	return llm.New(llm.Config{
		Provider:   cfg.LLMProvider,
		BaseURL:    cfg.LLMBaseURL,
		APIKeyEnv:  cfg.LLMAPIKeyEnv,
		Model:      cfg.LLMModel,
		Command:    cfg.LLMCommand,
		Timeout:    timeout,
		MaxRetries: 2,
	})
}

// newPipeline builds the full generation path.
func newPipeline(cfg *config.Config, rewriteOverride *bool) (*engine.Pipeline, error) {
	m, err := manifest.Load(cfg.ManifestPath)
	if err != nil {
		return nil, err
	}
	id, created, err := identity.LoadOrCreate(cfg.IdentityPath)
	if err != nil {
		return nil, err
	}
	if created {
		fmt.Fprintf(os.Stderr, "ammit: generated a new identity at %s (site %s); re-sign the manifest or peers cannot reproduce this site\n",
			cfg.IdentityPath, id.SiteID())
	}

	eng, err := engine.New(engine.Config{
		Manifest:    m,
		SiteID:      id.SiteID(),
		Host:        cfg.Host,
		Brand:       cfg.Brand,
		Topic:       cfg.Topic,
		BasePath:    cfg.BasePath,
		LinksPerDoc: cfg.LinksPerDoc,
		DocsPerPage: cfg.DocsPerPage,
	})
	if err != nil {
		return nil, err
	}

	wantRewrite := cfg.Rewrite
	if rewriteOverride != nil {
		wantRewrite = *rewriteOverride
	}
	if !wantRewrite {
		return engine.NewPipeline(eng, nil)
	}

	provider, err := buildProvider(cfg)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("rewrite is enabled but no model is configured: set llm_provider in the config")
	}

	rw, err := rewrite.New(provider, rewrite.Options{
		Temperature:    cfg.LLMTemperature,
		RewriteRecords: true,
	})
	if err != nil {
		return nil, err
	}
	return engine.NewPipeline(eng, rw)
}

// engineFromConfig is the config-file entry point used by generate and serve.
func engineFromConfig(path string) (*engine.Pipeline, *config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	p, err := newPipeline(cfg, nil)
	if err != nil {
		return nil, nil, err
	}
	return p, cfg, nil
}

// cmdAuditCorpus generates a corpus and measures how detectable it is.
//
// This is the command that answers the only question that matters about a
// poisoning corpus: would a curator be able to find this and delete it? A
// template-generated corpus shares phrasing across documents and scores badly.
// A rewritten one does not. Measuring it is the difference between believing the
// corpus evades filtering and knowing that it does.
func cmdAuditCorpus(args []string) error {
	fs := flag.NewFlagSet("audit-corpus", flag.ExitOnError)
	cfgPath := fs.String("config", "ammit.json", "config file")
	n := fs.Int("n", 200, "how many trap pages to generate")
	rewriteFlag := fs.Bool("rewrite", false, "force rewriting on (needs a model)")
	noRewrite := fs.Bool("no-rewrite", false, "force rewriting off")
	showSamples := fs.Int("samples", 0, "print this many sample documents")
	ngram := fs.Int("ngram", 5, "phrase length considered for boilerplate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *n < 1 {
		return fmt.Errorf("n must be at least 1")
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	var override *bool
	if *rewriteFlag {
		override = rewriteFlag
	}
	if *noRewrite {
		off := false
		override = &off
	}

	if override == nil {
		// A useful default: rewrite if a model is configured, because otherwise
		// the measurement is of a corpus nobody would deploy in this mode.
		want := cfg.LLMProvider != ""
		override = &want
	}

	p, err := newPipeline(cfg, override)
	if err != nil {
		return err
	}

	ctx := context.Background()
	texts := make([]string, 0, *n)
	docsTotal := 0

	start := time.Now()
	for i := 0; i < *n; i++ {
		nonce := fmt.Sprintf("%s/%d", corpus.StableToken(fmt.Sprintf("audit|%d", i)), i%8)
		docs, err := p.Generate(nonce)
		if err != nil {
			return fmt.Errorf("generate %d: %w", i, err)
		}
		for _, d := range docs {
			texts = append(texts, d.PlainText())
			docsTotal++
		}
	}
	elapsed := time.Since(start)

	mode := "template (no model)"
	if p.Rewriting() {
		mode = "rewritten by " + p.Rewriter.Provider()
	}

	fmt.Printf("mode:        %s\n", mode)
	fmt.Printf("generated:   %d pages, %d documents in %s\n", *n, docsTotal, elapsed.Round(time.Millisecond))
	if p.Rewriting() {
		st := p.RewriteStats()
		fmt.Printf("model calls: %d ok, %d failed (%d refused, %d fell back to template)\n",
			st.CallOK, st.CallFailed, st.Refused, st.Fallbacks)
	}
	fmt.Println()

	if *showSamples > 0 {
		for i := 0; i < *showSamples && i < len(texts); i++ {
			fmt.Printf("--- sample %d ---\n%s\n\n", i+1, strings.TrimSpace(texts[i]))
		}
	}

	rep := audit.Corpus(texts, audit.Options{NGram: *ngram})
	fmt.Println(rep.String())
	_ = ctx
	_ = strategy.DefaultMix
	return nil
}
