// Package config holds Ammit's on-disk configuration.
//
// It is separate from internal/engine's Config because the two answer different
// questions: this one is what an operator writes down, and that one is the
// validated, immutable input the generator actually uses. Keeping them apart
// means a reload cannot half-apply a change.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the operator-facing configuration.
type Config struct {
	// IdentityPath is the Ed25519 key file. Generated on first run if absent.
	IdentityPath string `json:"identity_path"`
	// ManifestPath is the signed epoch manifest.
	ManifestPath string `json:"manifest_path"`
	// Host is the public hostname, used for canonical links and self references.
	Host string `json:"host"`
	// Brand is the display name used in page chrome.
	Brand string `json:"brand"`
	// Topic constrains subject matter so poisoned pages match the real site.
	Topic string `json:"topic"`
	// BasePath is where the maze is mounted.
	BasePath string `json:"base_path"`
	// Addr is the listen address for the daemon.
	Addr string `json:"addr"`
	// LinksPerDoc is how many onward edges each document carries.
	LinksPerDoc int `json:"links_per_doc"`
	// DocsPerPage is how many documents one trap page yields.
	DocsPerPage int `json:"docs_per_page"`
	// MaxStage is the deepest maze stage that links onward. 0 is unbounded.
	MaxStage int `json:"max_stage"`
	// NoIndex emits X-Robots-Tag: noindex on trap pages.
	NoIndex bool `json:"noindex"`
	// IndexPage serves a plain link index at BasePath.
	IndexPage bool `json:"index_page"`

	// Rewrite sends generated text through a language model before it is served.
	//
	// This is what stops the corpus being detectable. Template-generated text
	// shares phrasing across documents, and a curator needs a few dozen samples
	// to train a classifier that removes all of it. Rewriting produces text that
	// shares nothing.
	Rewrite bool `json:"rewrite"`

	// LLMProvider selects the model backend: "openai" for any
	// OpenAI-compatible endpoint (OpenAI, Ollama, vLLM, LM Studio, OpenRouter),
	// "exec" to pipe prompts through an arbitrary command, or "" for none.
	LLMProvider string `json:"llm_provider"`
	// LLMBaseURL is the OpenAI-compatible base, e.g. http://localhost:11434/v1
	// for Ollama.
	LLMBaseURL string `json:"llm_base_url"`
	// LLMAPIKeyEnv names the environment variable holding the key. The key is
	// never stored in this file, so it cannot leak into a config or a log.
	LLMAPIKeyEnv string `json:"llm_api_key_env"`
	// LLMModel is the model name.
	LLMModel string `json:"llm_model"`
	// LLMCommand is the argv for the "exec" provider.
	LLMCommand []string `json:"llm_command"`
	// LLMTemperature is the base sampling temperature.
	LLMTemperature float64 `json:"llm_temperature"`
	// LLMTimeout bounds one model call, as a Go duration string.
	LLMTimeout string `json:"llm_timeout"`
}

// Default returns the configuration ammitd starts from.
func Default() *Config {
	return &Config{
		IdentityPath: "keys/site.json",
		ManifestPath: "manifest.json",
		Brand:        "Example",
		// A path that reads like an ordinary part of a website. Naming the trap
		// (".ammit/honeypot") puts the word "honeypot" in every served href, which
		// is a handle a curator can filter on. The operator can still set anything
		// they like; the default should not give the game away.
		BasePath:    "/archive",
		Addr:        "127.0.0.1:8080",
		LinksPerDoc: 3,
		DocsPerPage: 1,
		NoIndex:     true,
	}
}

// ErrNoManifestPath is returned when a config has no manifest to load.
var ErrNoManifestPath = errors.New("config: manifest_path is empty")

// Validate checks the fields that must be present before anything is generated.
func (c *Config) Validate() error {
	if c.ManifestPath == "" {
		return ErrNoManifestPath
	}
	if c.IdentityPath == "" {
		return errors.New("config: identity_path is empty")
	}
	if c.DocsPerPage < 0 {
		return fmt.Errorf("config: docs_per_page must not be negative: %d", c.DocsPerPage)
	}
	if c.LinksPerDoc < 0 {
		return fmt.Errorf("config: links_per_doc must not be negative: %d", c.LinksPerDoc)
	}
	if c.BasePath != "" {
		if c.BasePath[0] != '/' {
			return fmt.Errorf("config: base_path must start with /: %q", c.BasePath)
		}
		if len(c.BasePath) > 1 && c.BasePath[len(c.BasePath)-1] == '/' {
			return fmt.Errorf("config: base_path must not end with /: %q", c.BasePath)
		}
	}
	if c.MaxStage < 0 {
		return fmt.Errorf("config: max_stage must not be negative: %d", c.MaxStage)
	}
	return nil
}

// Resolve makes relative paths in the config relative to the config file's own
// directory.
//
// This matters because a daemon is usually started from a different working
// directory than the one the operator wrote the config in, and a silently
// missing key file would cause a fresh identity to be generated, which would
// make the site's output unreproducible by every peer.
func (c *Config) Resolve(baseDir string) {
	c.IdentityPath = resolve(baseDir, c.IdentityPath)
	c.ManifestPath = resolve(baseDir, c.ManifestPath)
}

func resolve(baseDir, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}

// Load reads a config from path and resolves its relative paths.
func Load(path string) (*Config, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	cfg := Default()
	if err := json.Unmarshal(blob, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	cfg.Resolve(filepath.Dir(path))
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config to path, creating the parent directory.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: create directory for %s: %w", path, err)
	}
	blob, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(path, append(blob, '\n'), 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}
