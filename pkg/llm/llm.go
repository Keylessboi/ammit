// Package llm is Ammit's model-access layer.
//
// It knows how to send a prompt to a text completion backend and how to tell a
// real completion from a refusal or an empty response. It deliberately knows
// nothing about Ammit's strategies or corpora: those packages depend on this
// one, never the other way around, so a rewrite can be pointed at a different
// backend without touching generation logic.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Request is one completion.
type Request struct {
	// System is the system instruction; it may be empty, in which case no
	// system message is sent at all.
	System string
	// Prompt is the user message.
	Prompt string
	// Temperature is the sampling temperature. Zero means "provider default"
	// and the field is omitted from the request.
	Temperature float64
	// MaxTokens caps the completion length. Zero means "provider default" and
	// the field is omitted from the request.
	MaxTokens int
	// Seed is an optional sampling seed. Providers that support one use it to
	// make a rewrite reproducible; others ignore it.
	Seed *int64
}

// Provider is a text completion backend.
type Provider interface {
	// Name identifies the provider for logs and for error messages.
	Name() string
	// Complete returns the model's completion.
	//
	// It must respect ctx cancellation and must not retry indefinitely: only
	// the transient failures it is configured to retry, with a bounded number
	// of attempts.
	Complete(ctx context.Context, req Request) (string, error)
}

// Config selects and configures a Provider.
type Config struct {
	// Provider is "openai", "exec", "echo" or "" (which means "none").
	Provider string
	// BaseURL is the OpenAI-compatible base, e.g. http://localhost:11434/v1
	// for Ollama, http://localhost:8000/v1 for vLLM, https://api.openai.com/v1.
	BaseURL string
	// APIKeyEnv names the environment variable holding the API key. The key is
	// NEVER stored in Config itself, so it cannot leak into a config file or a
	// log line. It is read from the environment at call time, which means a key
	// exported after New still takes effect.
	APIKeyEnv string
	// Model is the model name.
	Model string
	// Command is the argv for the "exec" provider. The prompt is written to
	// its stdin and the completion read from its stdout.
	Command []string
	// Timeout bounds one Complete call. Zero means 120s.
	Timeout time.Duration
	// MaxRetries is the number of retries on transient failure. Zero means 2.
	MaxRetries int
}

const (
	// defaultTimeout bounds a completion when Config.Timeout is zero. Model
	// endpoints are frequently slow, but an unbounded call would hang a
	// generation run forever.
	defaultTimeout = 120 * time.Second
	// defaultMaxRetries is the retry budget when Config.MaxRetries is zero.
	defaultMaxRetries = 2
)

// ErrNoProvider is returned by Complete-style calls when none is configured.
var ErrNoProvider = errors.New("llm: no provider configured")

// ErrRefused is returned when a provider's response looks like a refusal
// rather than a completion, so callers can distinguish "the model declined"
// from "the model returned nothing" or "the request failed".
var ErrRefused = errors.New("llm: provider refused")

// New builds a Provider. An empty Config.Provider returns (nil, nil) so a
// caller can treat "no model configured" as a valid state rather than an
// error.
func New(cfg Config) (Provider, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "":
		return nil, nil
	case "openai":
		return newOpenAI(cfg), nil
	case "exec":
		return newExec(cfg)
	case "echo":
		return echoProvider{}, nil
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", cfg.Provider)
	}
}

// refusalOpenings are the phrasings that mark a response as a decline. They are
// matched case-insensitively at the start of the completion.
var refusalOpenings = []string{
	"i can't",
	"i cannot",
	"i'm sorry",
	"i am unable",
	"i won't",
	"i'm not able",
	"i must decline",
}

// maxRefusalLen bounds how long a completion may be and still count as a
// refusal. A real decline is one or two sentences; a long answer that merely
// opens with "I'm sorry" is a completion, not a refusal, and must not be
// discarded.
const maxRefusalLen = 100

// checkCompletion validates model output shared by the HTTP providers and
// returns it unchanged when it is a real completion.
func checkCompletion(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errors.New("llm: empty completion")
	}
	if isRefusal(text) {
		return "", fmt.Errorf("llm: refusal: %q: %w", firstBytes([]byte(text), 200), ErrRefused)
	}
	return text, nil
}

// isRefusal reports whether text opens with a known refusal phrase and is short
// enough to be a decline rather than an answer.
func isRefusal(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > maxRefusalLen {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, opening := range refusalOpenings {
		if strings.HasPrefix(lower, opening) {
			return true
		}
	}
	return false
}

// firstBytes returns at most limit bytes of b, trimmed, for an error message.
// The point is to keep a hostile or verbose body out of a log line.
func firstBytes(b []byte, limit int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}

// identityProvider returns the prompt unchanged.
type identityProvider struct{}

// Name identifies the provider for logs and error messages.
func (identityProvider) Name() string { return "identity" }

// Complete returns the prompt unchanged.
func (identityProvider) Complete(_ context.Context, req Request) (string, error) {
	return req.Prompt, nil
}

// Identity returns a Provider that returns the prompt unchanged. It exists so
// that the rewrite path can be exercised with no model available.
func Identity() Provider { return identityProvider{} }
