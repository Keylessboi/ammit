package llm

import (
	"context"
	"fmt"
)

// echoText is the completion every echo provider returns. It is a constant, not
// a function of the request, so a test can assert on it exactly.
const echoText = "echo"

// echoProvider returns a fixed completion. It exists so tests can exercise code
// that depends on a Provider without a model, a network or a subprocess.
type echoProvider struct{}

// Name identifies the provider for logs and error messages.
func (echoProvider) Name() string { return "echo" }

// Complete returns the fixed echo completion.
func (echoProvider) Complete(ctx context.Context, _ Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("llm: echo: %w", err)
	}
	return echoText, nil
}
