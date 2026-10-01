package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// execProvider runs a command and treats it as a completion backend: the prompt
// goes to stdin and the completion is whatever lands on stdout. It is how a
// local CLI model is wired in without any HTTP dependency.
type execProvider struct {
	command []string
	timeout time.Duration
}

// newExec validates the argv up front, so a misconfigured provider fails at
// construction rather than on the first generation.
func newExec(cfg Config) (*execProvider, error) {
	if len(cfg.Command) == 0 || strings.TrimSpace(cfg.Command[0]) == "" {
		return nil, errors.New("llm: exec: command is empty")
	}
	command := make([]string, len(cfg.Command))
	copy(command, cfg.Command)
	return &execProvider{command: command, timeout: cfg.Timeout}, nil
}

// Name identifies the provider for logs and error messages.
func (p *execProvider) Name() string { return "exec" }

// Complete runs the command once with the prompt on stdin.
func (p *execProvider) Complete(ctx context.Context, req Request) (string, error) {
	// A timeout is enforced by the derived context: exec.CommandContext kills
	// the child when it expires, which is the only reliable way to bound a
	// command that ignores signals by finishing on its own.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.command[0], p.command[1:]...)
	cmd.Stdin = strings.NewReader(req.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// The exit status alone rarely explains a CLI model failure; the first
		// bytes of stderr are where its real complaint lives.
		if msg := firstBytes(stderr.Bytes(), 500); msg != "" {
			return "", fmt.Errorf("llm: exec: %w: %s", err, msg)
		}
		return "", fmt.Errorf("llm: exec: %w", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		return "", errors.New("llm: exec: empty completion")
	}
	return stdout.String(), nil
}
