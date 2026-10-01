// Command ammitd serves Ammit's trap surface as a standalone HTTP server.
//
// It exists for hosts that are not Anubis. An Anubis deployment has no need for
// it: the adapter integrates in-process and reuses Anubis's own mux, store and
// honeypot dispatch, which is strictly better. This binary is for everything
// else: an nginx location block, a Caddy route, a Cloudflare Worker origin, or a
// quick local look at what the corpus engine produces.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Keylessboi/ammit/pkg/config"
	"github.com/Keylessboi/ammit/pkg/engine"
	"github.com/Keylessboi/ammit/pkg/httpsrv"
	"github.com/Keylessboi/ammit/pkg/identity"
	"github.com/Keylessboi/ammit/pkg/manifest"
)

// Version is the build version, overridable at link time.
var Version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ammitd: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("ammitd", flag.ExitOnError)
	cfgPath := fs.String("config", "ammit.json", "config file")
	addr := fs.String("addr", "", "listen address, overriding the config")
	logLevel := fs.String("log", "info", "log level: debug, info, warn or error")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("ammitd", Version)
		return nil
	}

	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", *logLevel, err)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	listen := cfg.Addr
	if *addr != "" {
		listen = *addr
	}
	if listen == "" {
		return errors.New("no listen address: set addr in the config or pass -addr")
	}

	m, err := manifest.Load(cfg.ManifestPath)
	if err != nil {
		return err
	}
	id, created, err := identity.LoadOrCreate(cfg.IdentityPath)
	if err != nil {
		return err
	}
	if created {
		// Worth a warning rather than a debug line: a site that generates a fresh
		// identity is a site whose output no peer can reproduce, because the
		// derivation depends on the site id.
		slog.Warn("generated a new site identity; peers cannot reproduce this site's output until the manifest is re-signed",
			"site", id.SiteID(), "path", cfg.IdentityPath)
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
		return err
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
		return err
	}

	mux := http.NewServeMux()
	for _, r := range srv.Routes() {
		mux.Handle(r.Pattern, r.Handler)
		slog.Info("mounted", "pattern", r.Pattern)
	}

	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Shut down on signal so a container or systemd unit stops promptly rather
	// than being killed mid-request.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("ammitd listening",
			"addr", listen, "epoch", eng.Epoch(), "site", eng.SiteID(), "version", Version)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
