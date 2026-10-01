package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// defaultOpenAIBaseURL is used when Config.BaseURL is empty, so that providing
// only a model and a key is enough to talk to OpenAI itself.
const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// maxResponseBytes bounds a completion body. A broken or hostile endpoint must
// not be able to exhaust memory, so the body is read through an io.LimitReader.
const maxResponseBytes = 4 << 20

// maxBackoff caps the exponential retry pause. The default budget is two
// retries, but a caller who raises MaxRetries should still not end up sleeping
// for minutes between attempts.
const maxBackoff = 10 * time.Second

// chatMessage is one message in the OpenAI chat format.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the body POSTed to /chat/completions. Zero-valued optional
// fields are omitted so the server applies its own default rather than being
// told to use zero temperature or a zero token cap.
type chatRequest struct {
	Model       string        `json:"model,omitempty"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Seed        *int64        `json:"seed,omitempty"`
}

// chatResponse is the subset of the completion response that matters.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// openAIProvider talks to any OpenAI-compatible /chat/completions endpoint:
// OpenAI, Ollama, vLLM, LM Studio, llama.cpp's server, OpenRouter and most
// others. That is why it is the primary provider.
type openAIProvider struct {
	baseURL    string
	apiKeyEnv  string
	model      string
	timeout    time.Duration
	maxRetries int
	client     *http.Client
}

// newOpenAI builds the provider. It never resolves Config.APIKeyEnv here: the
// key is read at call time so a key exported after construction still works.
func newOpenAI(cfg Config) *openAIProvider {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	return &openAIProvider{
		baseURL:    base,
		apiKeyEnv:  cfg.APIKeyEnv,
		model:      cfg.Model,
		timeout:    cfg.Timeout,
		maxRetries: cfg.MaxRetries,
		client:     &http.Client{Timeout: cfg.Timeout},
	}
}

// Name identifies the provider for logs and error messages.
func (p *openAIProvider) Name() string { return "openai" }

// Complete sends one completion request, retrying only transient failures.
func (p *openAIProvider) Complete(ctx context.Context, req Request) (string, error) {
	messages := make([]chatMessage, 0, 2)
	if req.System != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})

	body, err := json.Marshal(chatRequest{
		Model:       p.model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Seed:        req.Seed,
	})
	if err != nil {
		return "", fmt.Errorf("llm: openai: encode request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoff(attempt)); err != nil {
				return "", fmt.Errorf("llm: openai: %w", err)
			}
		}
		text, retry, err := p.do(ctx, body)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if !retry {
			return "", err
		}
	}
	return "", fmt.Errorf("llm: openai: retries exhausted: %w", lastErr)
}

// do performs a single request. The retry flag reports whether the failure was
// transient: network faults and HTTP 429/5xx are worth another attempt, while
// 4xx responses are the caller's problem and would fail identically.
func (p *openAIProvider) do(ctx context.Context, body []byte) (string, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", false, fmt.Errorf("llm: openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// The key is looked up now, never stored on the provider or in an error.
	// An empty value means an unauthenticated endpoint, which is normal for a
	// local Ollama or llama.cpp server.
	if p.apiKeyEnv != "" {
		if key := os.Getenv(p.apiKeyEnv); key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// A cancelled or expired context is the caller's decision, not a
		// transient fault, so it must not be retried.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", false, fmt.Errorf("llm: openai: %w", ctxErr)
		}
		return "", true, fmt.Errorf("llm: openai: request: %w", err)
	}
	defer resp.Body.Close()

	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return "", true, fmt.Errorf("llm: openai: read response: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		msg := firstBytes(data, 500)
		if msg == "" {
			msg = "(empty body)"
		}
		return "", retryableStatus(resp.StatusCode),
			fmt.Errorf("llm: openai: status %d: %s", resp.StatusCode, msg)
	}

	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", false, fmt.Errorf("llm: openai: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", false, errors.New("llm: openai: response carried no choices")
	}
	text, err := checkCompletion(out.Choices[0].Message.Content)
	if err != nil {
		return "", false, err
	}
	return text, false, nil
}

// retryableStatus reports whether an HTTP status is worth retrying. 429 is a
// rate limit and the 5xx codes are server-side or gateway faults; the remaining
// 4xx codes describe the request itself and would fail again unchanged.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// backoff returns the pause before retry attempt n (1-based): 250ms, 750ms and
// three times that thereafter, capped at maxBackoff.
func backoff(attempt int) time.Duration {
	d := 250 * time.Millisecond
	for i := 1; i < attempt && d < maxBackoff; i++ {
		d *= 3
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

// sleepCtx waits for d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
