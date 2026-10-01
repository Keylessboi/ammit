package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// writeJSON renders a response body without hand-escaped JSON, so the tests
// stay readable and cannot be broken by a stray backslash.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

// completion builds a minimal OpenAI chat response carrying content.
func completion(content string) map[string]any {
	return map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{"role": "assistant", "content": content},
			},
		},
	}
}

func TestNewEmptyProviderIsNil(t *testing.T) {
	p, err := New(Config{Provider: ""})
	if err != nil {
		t.Fatalf("New(\"\"): unexpected error: %v", err)
	}
	if p != nil {
		t.Fatalf("New(\"\"): want nil provider, got %T", p)
	}
}

func TestNewUnknownProviderFails(t *testing.T) {
	if _, err := New(Config{Provider: "nope"}); err == nil {
		t.Fatal("New(nope): want error, got nil")
	}
}

func TestOpenAISuccessRoundTrip(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody chatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		writeJSON(w, completion("a genuinely novel rewrite"))
	}))
	defer srv.Close()

	t.Setenv("AMMIT_TEST_KEY", "sekret")
	p, err := New(Config{
		Provider:  "openai",
		BaseURL:   srv.URL,
		APIKeyEnv: "AMMIT_TEST_KEY",
		Model:     "ammit-test",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Name() != "openai" {
		t.Fatalf("Name() = %q, want openai", p.Name())
	}

	seed := int64(42)
	got, err := p.Complete(context.Background(), Request{
		System:      "rewrite without copying",
		Prompt:      "hello",
		Temperature: 0.7,
		MaxTokens:   64,
		Seed:        &seed,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "a genuinely novel rewrite" {
		t.Fatalf("Complete = %q, want %q", got, "a genuinely novel rewrite")
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer sekret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sekret")
	}
	if gotBody.Model != "ammit-test" {
		t.Errorf("model = %q, want ammit-test", gotBody.Model)
	}
	if len(gotBody.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(gotBody.Messages))
	}
	if gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != "rewrite without copying" {
		t.Errorf("system message = %+v", gotBody.Messages[0])
	}
	if gotBody.Messages[1].Role != "user" || gotBody.Messages[1].Content != "hello" {
		t.Errorf("user message = %+v", gotBody.Messages[1])
	}
	if gotBody.Temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", gotBody.Temperature)
	}
	if gotBody.MaxTokens != 64 {
		t.Errorf("max_tokens = %d, want 64", gotBody.MaxTokens)
	}
	if gotBody.Seed == nil || *gotBody.Seed != seed {
		t.Errorf("seed = %v, want %d", gotBody.Seed, seed)
	}
}

func TestOpenAIAuthorizationAbsentWithoutKey(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		writeJSON(w, completion("ok"))
	}))
	defer srv.Close()

	t.Setenv("AMMIT_TEST_KEY", "")
	p, err := New(Config{
		Provider:  "openai",
		BaseURL:   srv.URL,
		APIKeyEnv: "AMMIT_TEST_KEY",
		Model:     "local",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Complete(context.Background(), Request{Prompt: "hi"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none for an unauthenticated endpoint", gotAuth)
	}
	// Zero-valued optional fields must be omitted, and an empty system prompt
	// must not become a system message.
	if _, ok := gotBody["temperature"]; ok {
		t.Error("temperature was sent although it was zero")
	}
	if _, ok := gotBody["max_tokens"]; ok {
		t.Error("max_tokens was sent although it was zero")
	}
	if _, ok := gotBody["seed"]; ok {
		t.Error("seed was sent although it was nil")
	}
	messages, ok := gotBody["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v, want exactly one user message", gotBody["messages"])
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "user" {
		t.Errorf("message role = %v, want user", first["role"])
	}
}

func TestOpenAIReadsKeyAtCallTime(t *testing.T) {
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, completion("ok"))
	}))
	defer srv.Close()

	t.Setenv("AMMIT_TEST_KEY", "")
	p, err := New(Config{
		Provider:  "openai",
		BaseURL:   srv.URL,
		APIKeyEnv: "AMMIT_TEST_KEY",
		Model:     "local",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The key is exported only after construction; it must still be used.
	t.Setenv("AMMIT_TEST_KEY", "late-key")
	if _, err := p.Complete(context.Background(), Request{Prompt: "hi"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotAuth != "Bearer late-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer late-key")
	}
}

func TestOpenAIRetriesOn500(t *testing.T) {
	var calls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "transient", http.StatusInternalServerError)
			return
		}
		writeJSON(w, completion("recovered"))
	}))
	defer srv.Close()

	p, err := New(Config{Provider: "openai", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, MaxRetries: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "recovered" {
		t.Fatalf("Complete = %q, want recovered", got)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("handler calls = %d, want 2", n)
	}
}

func TestOpenAIDoesNotRetryOn400(t *testing.T) {
	var calls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	p, err := New(Config{Provider: "openai", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, MaxRetries: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Complete(context.Background(), Request{Prompt: "hi"}); err == nil {
		t.Fatal("Complete: want error for HTTP 400, got nil")
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("handler calls = %d, want 1 (400 must not be retried)", n)
	}
}

func TestOpenAIRefusalReturnsErrRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, completion("I can't help with that."))
	}))
	defer srv.Close()

	p, err := New(Config{Provider: "openai", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("Complete error = %v, want ErrRefused", err)
	}
	if got != "" {
		t.Fatalf("Complete returned %q alongside a refusal, want empty", got)
	}
}

func TestOpenAIEmptyCompletionIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, completion(""))
	}))
	defer srv.Close()

	p, err := New(Config{Provider: "openai", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("Complete: want error for an empty completion, got nil")
	}
	if errors.Is(err, ErrRefused) {
		t.Fatalf("empty completion must not be reported as a refusal: %v", err)
	}
}

func TestExecProviderPipeThroughCat(t *testing.T) {
	p, err := New(Config{Provider: "exec", Command: []string{"cat"}, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Name() != "exec" {
		t.Fatalf("Name() = %q, want exec", p.Name())
	}
	got, err := p.Complete(context.Background(), Request{Prompt: "hello from stdin"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "hello from stdin" {
		t.Fatalf("Complete = %q, want the prompt echoed back", got)
	}
}

func TestExecProviderReportsStderr(t *testing.T) {
	p, err := New(Config{
		Provider: "exec",
		Command:  []string{"sh", "-c", "echo boom >&2; exit 3"},
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("Complete: want error for a non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %v does not contain the command's stderr", err)
	}
}

func TestExecProviderRequiresCommand(t *testing.T) {
	if _, err := New(Config{Provider: "exec"}); err == nil {
		t.Fatal("New(exec without command): want error, got nil")
	}
}

func TestIdentityReturnsPrompt(t *testing.T) {
	p := Identity()
	if p.Name() != "identity" {
		t.Fatalf("Name() = %q, want identity", p.Name())
	}
	got, err := p.Complete(context.Background(), Request{Prompt: "unchanged"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "unchanged" {
		t.Fatalf("Complete = %q, want unchanged", got)
	}
}

func TestEchoProviderReturnsFixedString(t *testing.T) {
	p, err := New(Config{Provider: "echo"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := p.Complete(context.Background(), Request{Prompt: "ignored"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != echoText {
		t.Fatalf("Complete = %q, want %q", got, echoText)
	}
}
