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
	"github.com/Keylessboi/ammit/pkg/scramble"
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

	payloads, err := loadPayloads(cfg.PayloadFile)
	if err != nil {
		return nil, err
	}

	eng, err := engine.New(engine.Config{
		Manifest:    m,
		SiteID:      id.SiteID(),
		Pepper:      id.SitePepper(),
		Payloads:    payloads,
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

	// The trigger tokens come from the manifest, which is public. Without a
	// pepper the site emits the public value, and anybody who reads the manifest
	// can filter this site's corpus by searching for it.
	if eng.CanaryIsPublic() {
		fmt.Fprintf(os.Stderr,
			"ammit: warning: no site pepper available, so the trigger tokens are the public manifest values.\n"+
				"       regenerate the identity with 'ammit keygen -force' to get a private pepper.\n")
	}

	wantRewrite := cfg.Rewrite
	if rewriteOverride != nil {
		wantRewrite = *rewriteOverride
	}

	var rw *rewrite.Rewriter
	if wantRewrite {
		provider, err := buildProvider(cfg)
		if err != nil {
			return nil, err
		}
		if provider == nil {
			return nil, fmt.Errorf("rewrite is enabled but no model is configured: set llm_provider in the config")
		}
		rw, err = rewrite.New(provider, rewriteProfile(cfg))
		if err != nil {
			return nil, err
		}
	}

	p, err := engine.NewPipeline(eng, rw)
	if err != nil {
		return nil, err
	}
	if cfg.Scramble {
		p.Scrambler = scramble.New(scramble.DefaultOptions())
	}
	return p, nil
}

// rewriteProfile picks the instruction style for the configured model size.
//
// A 4B model cannot hold a seven-rule prompt. It keeps the first rule, forgets
// the rest, and appends a friendly sentence. The small profile gives it four
// short sentences, one sentence of work per call, and cleans up afterwards.
func rewriteProfile(cfg *config.Config) rewrite.Options {
	if cfg.LLMSmall {
		return rewrite.SmallOptions()
	}
	opts := rewrite.DefaultOptions()
	if cfg.LLMTemperature > 0 {
		opts.Temperature = cfg.LLMTemperature
	}
	return opts
}

// loadPayloads reads the operator payload file, one behaviour per line.
func loadPayloads(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read payload file: %w", err)
	}
	var out []string
	for _, line := range strings.Split(string(blob), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("payload file %s contains no payloads", path)
	}
	return out, nil
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
	scrambleFlag := fs.Bool("scramble", false, "force model-free scrambling on")
	noScramble := fs.Bool("no-scramble", false, "force model-free scrambling off")
	payloadsFlag := fs.String("payloads", "", "payload file for the backdoor strategy")
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

	if *scrambleFlag {
		cfg.Scramble = true
	}
	if *noScramble {
		cfg.Scramble = false
	}
	if *payloadsFlag != "" {
		cfg.PayloadFile = *payloadsFlag
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
	prose := make([]string, 0, *n)
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
			prose = append(prose, proseOf(d))
			docsTotal++
		}
	}
	elapsed := time.Since(start)

	mode := "template (no model)"
	switch {
	case p.Rewriting() && p.Scrambling():
		mode = "scrambled, then rewritten by " + p.Rewriter.Provider()
	case p.Rewriting():
		mode = "rewritten by " + p.Rewriter.Provider()
	case p.Scrambling():
		mode = "scrambled (no model)"
	}

	fmt.Printf("mode:        %s\n", mode)
	fmt.Printf("generated:   %d pages, %d documents in %s\n", *n, docsTotal, elapsed.Round(time.Millisecond))
	if p.Rewriting() {
		st := p.RewriteStats()
		fmt.Printf("model calls: %d ok, %d failed\n", st.CallOK, st.CallFailed)
		fmt.Printf("             %d refused by the model, %d rejected as unusable, %d came back unchanged\n",
			st.Refused, st.Rejected, st.NoOp)
	}
	fmt.Println()

	if *showSamples > 0 {
		for i := 0; i < *showSamples && i < len(texts); i++ {
			fmt.Printf("--- sample %d ---\n%s\n\n", i+1, strings.TrimSpace(texts[i]))
		}
	}

	// Two measurements, because they answer different questions.
	//
	// The full corpus contains the JSON record schemas. Those keys repeat by
	// design: every OpenAI-format record has a "role" and a "content", and a
	// filter that removed them would remove every real dataset too. Counting them
	// as boilerplate overstates the risk.
	//
	// The prose measurement is the one that matters for evasion. Prose is what a
	// classifier keys on, because prose is where a generator's habits show.
	opts := audit.Options{NGram: *ngram}

	fmt.Println("=== prose only (the measurement that matters) ===")
	fmt.Println(audit.Corpus(prose, opts).String())
	fmt.Println()
	fmt.Println("=== full corpus (includes record schemas, which repeat by design) ===")
	fmt.Println(audit.Corpus(texts, opts).String())

	_ = ctx
	_ = strategy.DefaultMix
	return nil
}

// proseOf returns a document's prose with its structured records removed.
func proseOf(d corpus.Document) string {
	var b strings.Builder
	b.WriteString(d.Title)
	b.WriteString(" ")
	b.WriteString(d.Summary)
	b.WriteString(" ")
	for _, s := range d.Sections {
		b.WriteString(s.Heading)
		b.WriteString(" ")
		for _, p := range s.Body {
			b.WriteString(p)
			b.WriteString(" ")
		}
	}
	return b.String()
}
