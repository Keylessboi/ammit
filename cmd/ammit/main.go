// Command ammit is the operator interface to the Ammit poisoning subsystem.
//
// It covers the whole lifecycle: creating a site identity, publishing and
// signing an epoch manifest, generating poisoned documents for inspection or
// for feeding an external pipeline, auditing text for watermark markers, and
// serving the trap surface.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Keylessboi/ammit/pkg/config"
	"github.com/Keylessboi/ammit/pkg/corpus"
	"github.com/Keylessboi/ammit/pkg/engine"
	"github.com/Keylessboi/ammit/pkg/httpsrv"
	"github.com/Keylessboi/ammit/pkg/identity"
	"github.com/Keylessboi/ammit/pkg/manifest"
	"github.com/Keylessboi/ammit/pkg/strategy"
)

// Version is the build version, overridable at link time.
var Version = "0.1.0-dev"

const usage = `ammit - training-data poisoning subsystem for Anubis

usage: ammit <command> [flags]

commands:
  keygen       create this site's Ed25519 identity
  init         write a default config file
  manifest     create, sign, verify or inspect an epoch manifest
  generate     emit poisoned documents for a nonce
  audit        check text for watermark markers
  strategies   list the available strategies
  serve        run the trap surface
  version      print the version

run 'ammit <command> -h' for the flags of a command
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "init":
		err = cmdInit(os.Args[2:])
	case "manifest":
		err = cmdManifest(os.Args[2:])
	case "generate":
		err = cmdGenerate(os.Args[2:])
	case "audit":
		err = cmdAudit(os.Args[2:])
	case "strategies":
		err = cmdStrategies(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("ammit", Version)
		return
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "ammit: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "ammit: %v\n", err)
		os.Exit(1)
	}
}

// keygen creates an identity and prints the resulting site id.
func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("o", "keys/site.json", "where to write the identity")
	force := fs.Bool("force", false, "overwrite an existing identity")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !*force {
		if _, err := os.Stat(*out); err == nil {
			return fmt.Errorf("identity already exists at %s; use -force to replace it, which will change this site's site id and break reproducibility of what it has already served", *out)
		}
	}

	id, err := identity.Generate()
	if err != nil {
		return err
	}
	if err := id.Save(*out); err != nil {
		return err
	}

	fmt.Printf("site id:     %s\n", id.SiteID())
	fmt.Printf("public key:  %s\n", base64.StdEncoding.EncodeToString(id.PublicKey))
	fmt.Printf("written to:  %s\n", *out)
	fmt.Println()
	fmt.Println("Share only the public key. The manifest carries it automatically when you sign.")
	return nil
}

// init writes a default config.
func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	out := fs.String("o", "ammit.json", "where to write the config")
	host := fs.String("host", "", "public hostname")
	brand := fs.String("brand", "", "site display name")
	topic := fs.String("topic", "", "subject area, or empty to derive per page")
	force := fs.Bool("force", false, "overwrite an existing config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !*force {
		if _, err := os.Stat(*out); err == nil {
			return fmt.Errorf("config already exists at %s; use -force to replace it", *out)
		}
	}

	cfg := config.Default()
	if *host != "" {
		cfg.Host = *host
	}
	if *brand != "" {
		cfg.Brand = *brand
	}
	if *topic != "" {
		cfg.Topic = *topic
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.Save(*out); err != nil {
		return err
	}

	fmt.Printf("wrote %s\n", *out)
	fmt.Println("next: ammit keygen, then ammit manifest new -sign")
	return nil
}

// cmdManifest dispatches the manifest subcommands.
func cmdManifest(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("manifest needs a subcommand: new, sign, verify or show")
	}
	switch args[0] {
	case "new":
		return manifestNew(args[1:])
	case "sign":
		return manifestSign(args[1:])
	case "verify":
		return manifestVerify(args[1:])
	case "show":
		return manifestShow(args[1:])
	default:
		return fmt.Errorf("unknown manifest subcommand %q", args[0])
	}
}

// manifestNew creates an unsigned manifest and optionally signs it.
func manifestNew(args []string) error {
	fs := flag.NewFlagSet("manifest new", flag.ExitOnError)
	out := fs.String("o", "manifest.json", "where to write the manifest")
	epoch := fs.Uint64("epoch", 1, "epoch number")
	duration := fs.Duration("duration", 7*24*time.Hour, "how long the epoch lasts")
	keyPath := fs.String("key", "keys/site.json", "identity to sign with, or empty to leave unsigned")
	canaries := fs.String("canaries", "", "comma-separated trigger tokens; empty generates one")
	seedB64 := fs.String("seed", "", "base64 network seed; empty generates one")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var seedBytes []byte
	if *seedB64 != "" {
		b, err := base64.StdEncoding.DecodeString(*seedB64)
		if err != nil {
			return fmt.Errorf("decode seed: %w", err)
		}
		seedBytes = b
	} else {
		id, err := identity.Generate()
		if err != nil {
			return fmt.Errorf("generate entropy for seed: %w", err)
		}
		// The identity private key is 64 bytes of ed25519 material and is used
		// here only as a source of CSPRNG output. It is discarded immediately
		// and never written anywhere.
		seedBytes = id.PrivateKey.Seed()
	}

	var canaryList []string
	for _, c := range strings.Split(*canaries, ",") {
		if c = strings.TrimSpace(c); c != "" {
			canaryList = append(canaryList, c)
		}
	}
	if len(canaryList) == 0 {
		// A canary must be rare in ordinary text and stable across the epoch.
		// A short hex token satisfies both and does not read as a real word.
		id, err := identity.Generate()
		if err != nil {
			return err
		}
		canaryList = []string{corpus.StableToken(base64.StdEncoding.EncodeToString(id.PublicKey))}
	}

	m, err := manifest.New(seedBytes, *epoch, *duration,
		map[string]float64(strategy.DefaultMix()), canaryList)
	if err != nil {
		return err
	}

	if *keyPath != "" {
		id, err := identity.Load(*keyPath)
		if err != nil {
			return err
		}
		if err := m.Sign(id); err != nil {
			return err
		}
	}

	if err := m.Save(*out); err != nil {
		return err
	}

	fmt.Printf("epoch:      %d\n", m.Epoch)
	fmt.Printf("canary:     %s\n", strings.Join(canaryList, ", "))
	fmt.Printf("expires:    %s\n", m.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("signers:    %d\n", len(m.Signatures))
	fmt.Printf("written to: %s\n", *out)
	if *keyPath == "" {
		fmt.Println()
		fmt.Println("The manifest is unsigned. Peers will not accept it until it carries")
		fmt.Println("signatures from enough distinct sites: ammit manifest sign -m " + *out)
	}
	return nil
}

// manifestSign adds this site's signature.
func manifestSign(args []string) error {
	fs := flag.NewFlagSet("manifest sign", flag.ExitOnError)
	mPath := fs.String("m", "manifest.json", "manifest to sign")
	keyPath := fs.String("key", "keys/site.json", "identity to sign with")
	if err := fs.Parse(args); err != nil {
		return err
	}

	m, err := manifest.Load(*mPath)
	if err != nil {
		return err
	}
	id, err := identity.Load(*keyPath)
	if err != nil {
		return err
	}
	if err := m.Sign(id); err != nil {
		return err
	}
	if err := m.Save(*mPath); err != nil {
		return err
	}

	fmt.Printf("signed by %s\n", id.SiteID())
	fmt.Printf("distinct signers now: %d\n", len(m.SignerIDs()))
	return nil
}

// manifestVerify checks a manifest's signatures and window.
func manifestVerify(args []string) error {
	fs := flag.NewFlagSet("manifest verify", flag.ExitOnError)
	mPath := fs.String("m", "manifest.json", "manifest to verify")
	threshold := fs.Int("threshold", 1, "minimum number of distinct signers")
	if err := fs.Parse(args); err != nil {
		return err
	}

	m, err := manifest.Load(*mPath)
	if err != nil {
		return err
	}
	if err := m.VerifySignatures(); err != nil {
		return err
	}
	if err := m.VerifyThreshold(*threshold); err != nil {
		return err
	}
	if err := m.ValidAt(time.Now()); err != nil {
		return err
	}

	fmt.Printf("valid: %d distinct signers\n", len(m.SignerIDs()))
	for _, s := range m.SignerIDs() {
		fmt.Printf("  %s\n", s)
	}
	return nil
}

// manifestShow prints a manifest without verifying it.
func manifestShow(args []string) error {
	fs := flag.NewFlagSet("manifest show", flag.ExitOnError)
	mPath := fs.String("m", "manifest.json", "manifest to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := manifest.Load(*mPath)
	if err != nil {
		return err
	}
	blob, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(blob))
	return nil
}

// engineFromConfig builds an engine from a config file.
func engineFromConfig(path string) (*engine.Engine, *config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	m, err := manifest.Load(cfg.ManifestPath)
	if err != nil {
		return nil, nil, err
	}
	id, _, err := identity.LoadOrCreate(cfg.IdentityPath)
	if err != nil {
		return nil, nil, err
	}

	e, err := engine.New(engine.Config{
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
		return nil, nil, err
	}
	return e, cfg, nil
}

// cmdGenerate emits poisoned documents for a nonce.
func cmdGenerate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	cfgPath := fs.String("config", "ammit.json", "config file")
	nonce := fs.String("nonce", "", "page nonce, e.g. 3f9a2b/0")
	format := fs.String("format", "jsonl", "output format: jsonl, json or text")
	count := fs.Int("n", 1, "how many nonces to emit, numbered from the given one")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nonce == "" {
		// A nonce is the trap id; an operator inspecting output needs something
		// reproducible, so a fresh random one is supplied rather than an empty
		// string, and it is printed so the same output can be regenerated.
		id, err := identity.Generate()
		if err != nil {
			return err
		}
		*nonce = corpus.StableToken(base64.StdEncoding.EncodeToString(id.PublicKey)) + "/0"
		fmt.Fprintf(os.Stderr, "# generated nonce: %s\n", *nonce)
	}

	e, _, err := engineFromConfig(*cfgPath)
	if err != nil {
		return err
	}

	switch *format {
	case "jsonl", "json", "text":
	default:
		return fmt.Errorf("unknown format %q", *format)
	}

	var all []corpus.Document
	base := *nonce
	for i := 0; i < *count; i++ {
		n := base
		if i > 0 {
			// Vary the stage rather than the id so the documents form a maze
			// rather than unrelated pages.
			head := base
			if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
				head = base[:idx]
			}
			n = fmt.Sprintf("%s/%d", head, i)
		}
		docs, err := e.Generate(n)
		if err != nil {
			return err
		}
		all = append(all, docs...)
	}

	switch *format {
	case "jsonl":
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		for _, d := range all {
			row := map[string]any{
				"id":          d.Nonce,
				"strategy":    d.Strategy,
				"kind":        d.Kind,
				"title":       d.Title,
				"text":        d.PlainText(),
				"records":     d.Records,
				"meta":        d.Meta,
				"words":       d.WordCount(),
				"fingerprint": d.Fingerprint(),
			}
			if err := enc.Encode(row); err != nil {
				return err
			}
		}
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(all); err != nil {
			return err
		}
	default:
		for _, d := range all {
			fmt.Println(d.PlainText())
			fmt.Println(strings.Repeat("-", 72))
		}
	}
	return nil
}

// cmdAudit checks text for watermark markers.
func cmdAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	cfgPath := fs.String("config", "ammit.json", "config file")
	file := fs.String("f", "", "file to check, or - for stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("audit needs -f")
	}

	e, _, err := engineFromConfig(*cfgPath)
	if err != nil {
		return err
	}

	var blob []byte
	if *file == "-" {
		blob, err = readAll(os.Stdin)
	} else {
		blob, err = os.ReadFile(*file)
	}
	if err != nil {
		return err
	}

	m, err := manifest.Load(mustManifestPath(*cfgPath))
	if err != nil {
		return err
	}

	// The key must come from the engine, not be rebuilt here: the generator
	// derives it from the derivation fingerprint, and a detector that derived it
	// any other way would simply never fire while appearing to work.
	key, err := e.WatermarkKeyString()
	if err != nil {
		return err
	}
	res := strategy.Detect(key, string(blob))

	fmt.Printf("text words:   %d\n", res.Words)
	fmt.Printf("marker count: %d\n", res.Count)
	fmt.Printf("marker rate:  %.3f per 1000 words (threshold %.1f)\n", res.Rate, strategy.DetectThreshold)
	fmt.Printf("markers found: %s\n", strings.Join(res.Found, ", "))
	fmt.Printf("epoch:        %d\n", m.Epoch)
	if res.Detected {
		fmt.Println("verdict:      watermarked")
	} else {
		fmt.Println("verdict:      no markers above threshold")
	}
	return nil
}

func mustManifestPath(cfgPath string) string {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return "manifest.json"
	}
	return cfg.ManifestPath
}

// maxAuditBytes caps how much of an input the auditor reads, so that pointing it
// at a huge file cannot exhaust memory.
const maxAuditBytes = 64 << 20

// readAll reads a whole reader, capped.
func readAll(f *os.File) ([]byte, error) {
	blob, err := io.ReadAll(io.LimitReader(f, maxAuditBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	if len(blob) > maxAuditBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", maxAuditBytes)
	}
	return blob, nil
}

// cmdStrategies lists the registered strategies.
func cmdStrategies(args []string) error {
	fs := flag.NewFlagSet("strategies", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Printf("%-14s %-12s %s\n", "NAME", "FAMILY", "DESCRIPTION")
	for _, s := range strategy.All() {
		fmt.Printf("%-14s %-12s %s\n", s.Name(), s.Family(), s.Description())
	}
	fmt.Println()
	fmt.Println("Set the mix for an epoch in the manifest's strategy_mix field.")
	fmt.Println("Current default mix:")
	for k, v := range strategy.DefaultMix() {
		fmt.Printf("  %-14s %.2f\n", k, v)
	}
	return nil
}

// cmdServe runs the trap surface.
func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "ammit.json", "config file")
	addr := fs.String("addr", "", "override the listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	e, cfg, err := engineFromConfig(*cfgPath)
	if err != nil {
		return err
	}

	listen := cfg.Addr
	if *addr != "" {
		listen = *addr
	}

	srv, err := httpsrv.New(httpsrv.Config{
		BasePath:  cfg.BasePath,
		Host:      cfg.Host,
		Brand:     cfg.Brand,
		NoIndex:   cfg.NoIndex,
		MaxStage:  cfg.MaxStage,
		IndexPage: cfg.IndexPage,
	}, e)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	for _, route := range srv.Routes() {
		mux.Handle(route.Pattern, route.Handler)
		fmt.Printf("mounted %s\n", route.Pattern)
	}

	fmt.Printf("ammit serving on %s (epoch %d, site %s)\n", listen, e.Epoch(), e.SiteID())
	server := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}

// defaultConfigPath returns the config path from the environment or the default.
func defaultConfigPath() string {
	if p := os.Getenv("AMMIT_CONFIG"); p != "" {
		return p
	}
	return filepath.Join("ammit.json")
}
