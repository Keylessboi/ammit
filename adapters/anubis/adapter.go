// Package anubis wires Ammit into Anubis.
//
// It offers two tiers of integration, because Anubis exposes two different
// things:
//
//   - Tier 1, no patch. This package registers itself with Anubis's challenge
//     extension registry from init(). A site adds one blank import and Ammit's
//     trap surface is mounted on Anubis's own mux. Works against stock upstream
//     Anubis.
//
//   - Tier 2, small patch. NewHoneypot has the same shape as the shipped naive
//     honeypot, so Anubis's honeypot dispatch can call it when the config says
//     implementation: "ammit". This makes Ammit own the /honeypot/{id}/{stage}
//     maze that Anubis already links to from every challenge page.
//
// Tier 2 is the one that matters, because Anubis already plants a honeypot link
// in every challenge page and already penalises clients that follow it. Tier 1
// alone gets Ammit's content served but does not get it into that link.
package anubis

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	anubis "github.com/TecharoHQ/anubis"
	"github.com/TecharoHQ/anubis/lib/challenge"
	"github.com/TecharoHQ/anubis/lib/challenge/extension"
	"github.com/TecharoHQ/anubis/lib/store"
	"github.com/a-h/templ"

	"github.com/Keylessboi/ammit/pkg/config"
	"github.com/Keylessboi/ammit/pkg/engine"
	"github.com/Keylessboi/ammit/pkg/httpsrv"
	"github.com/Keylessboi/ammit/pkg/identity"
	"github.com/Keylessboi/ammit/pkg/manifest"
)

// Name is the registry key this extension registers under.
const Name = "ammit"

// EnvConfig names the environment variable that points at Ammit's config file.
//
// The extension is inert unless this variable is set or ./ammit.json exists. A
// blank import must not be able to break an Anubis that has never heard of
// Ammit, so the no-op path is the default and activation is explicit.
const EnvConfig = "AMMIT_CONFIG"

func init() { extension.Register(Name, &Extension{}) }

// Extension is the Tier-1 integration.
type Extension struct {
	once sync.Once
	srv  *httpsrv.Server
	err  error

	// enabled records whether a configuration was found. When false, Setup
	// registers nothing and returns nil.
	enabled bool
}

// Setup mounts Ammit's trap routes on Anubis's mux.
//
// It implements extension.Impl. Errors here are returned to Anubis, which
// aggregates them and refuses to start: an operator who pointed AMMIT_CONFIG at a
// broken file wants to hear about it at boot rather than discover later that the
// trap surface was silently absent.
func (e *Extension) Setup(mux *http.ServeMux, st store.Interface) error {
	e.once.Do(func() { e.srv, e.enabled, e.err = build() })
	if e.err != nil {
		return e.err
	}
	if !e.enabled {
		return nil
	}

	prefix := strings.TrimSuffix(anubis.BasePrefix, "/")
	for _, route := range e.srv.Routes() {
		mux.Handle(prefix+route.Pattern, route.Handler)
	}
	return nil
}

// Head returns additional markup for the challenge page head.
//
// It returns nothing on purpose. Anubis already plants the honeypot link in the
// challenge page and already weights clients that follow it, so there is nothing
// for Ammit to add here. Tier 2 is what turns that existing link into a delivery
// mechanism.
func (e *Extension) Head(r *http.Request, chall *challenge.Challenge) templ.Component {
	return templ.NopComponent
}

// Validate has no side effects to check. Ammit never mints a JWT; it only serves
// pages, so there is nothing here that could have been left half-done.
func (e *Extension) Validate(r *http.Request, lg *slog.Logger, in *challenge.ValidateInput) error {
	return nil
}

// Server returns the built trap server, or nil when Ammit is not configured.
// Tier 2 and tests use it.
func (e *Extension) Server() *httpsrv.Server {
	e.once.Do(func() { e.srv, e.enabled, e.err = build() })
	return e.srv
}

// Enabled reports whether a configuration was found.
func (e *Extension) Enabled() bool {
	e.once.Do(func() { e.srv, e.enabled, e.err = build() })
	return e.enabled
}

// Err returns any error from building the server.
func (e *Extension) Err() error {
	e.once.Do(func() { e.srv, e.enabled, e.err = build() })
	return e.err
}

// build loads the configuration and constructs the trap server.
//
// The second return value is false when no configuration exists, which is the
// ordinary case for an Anubis that merely has the package imported.
func build() (*httpsrv.Server, bool, error) {
	path := os.Getenv(EnvConfig)
	if path == "" {
		path = "ammit.json"
	}

	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) && os.Getenv(EnvConfig) == "" {
			// No config and no explicit request for one: stay inert.
			slog.Debug("ammit: no configuration found, extension is inactive", "path", path)
			return nil, false, nil
		}
		return nil, false, err
	}

	m, err := manifest.Load(cfg.ManifestPath)
	if err != nil {
		return nil, false, err
	}
	id, _, err := identity.LoadOrCreate(cfg.IdentityPath)
	if err != nil {
		return nil, false, err
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
		return nil, false, err
	}

	srv, err := httpsrv.New(httpsrv.Config{
		BasePath:  cfg.BasePath,
		Host:      cfg.Host,
		Brand:     cfg.Brand,
		NoIndex:   cfg.NoIndex,
		MaxStage:  cfg.MaxStage,
		IndexPage: cfg.IndexPage,
	}, eng)
	if err != nil {
		return nil, false, err
	}

	slog.Info("ammit: trap surface active",
		"base_path", cfg.BasePath, "epoch", eng.Epoch(), "site", eng.SiteID())
	return srv, true, nil
}
