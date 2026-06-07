// Package provider implements per-provider usage reading (zero-quota, via the
// OAuth usage endpoints) and window triggering (via the official CLIs).
package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/wavever/CCLimitPing/internal/usage"
)

// Provider abstracts a single AI coding provider.
type Provider interface {
	// Name is the stable identifier ("claude", "codex").
	Name() string
	// ReadUsage fetches the current rate-limit snapshot. This is a read-only
	// call against the provider's usage endpoint and consumes no quota.
	ReadUsage(ctx context.Context) (*usage.Usage, error)
	// Trigger sends a minimal message via the official CLI to start a new
	// window. When dryRun is true it fills only Command and executes nothing.
	Trigger(ctx context.Context, dryRun bool) (*TriggerResult, error)
}

// TriggerResult reports what a Trigger did, including the token usage the ping
// consumed (parsed from the CLI's machine-readable output). CostUSD is 0 when
// the provider doesn't report a cost (e.g. Codex).
type TriggerResult struct {
	Command      string
	HasUsage     bool
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostUSD      float64
}

// tokenSource is satisfied by the auth holders for both providers.
type tokenSource interface {
	Token(ctx context.Context) (string, error)
	Reload(ctx context.Context) (string, error)
	Refresh(ctx context.Context) (string, error)
}

// fetchWithAuth issues a GET built by buildReq using a token from src. On a 401
// it first reloads the credential store (the official CLI may have refreshed
// it) and, failing that, performs an OAuth refresh — each retried once. It
// returns the response body on success.
func fetchWithAuth(ctx context.Context, src tokenSource, buildReq func(token string) (*http.Request, error)) ([]byte, error) {
	token, err := src.Token(ctx)
	if err != nil {
		return nil, err
	}

	body, status, err := doGet(ctx, token, buildReq)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		if t, rerr := src.Reload(ctx); rerr == nil && t != token {
			token = t
			if body, status, err = doGet(ctx, token, buildReq); err != nil {
				return nil, err
			}
		}
	}
	if status == http.StatusUnauthorized {
		t, rerr := src.Refresh(ctx)
		if rerr != nil {
			return nil, fmt.Errorf("unauthorized and refresh failed: %w", rerr)
		}
		token = t
		if body, status, err = doGet(ctx, token, buildReq); err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("usage endpoint returned HTTP %d: %s", status, truncate(body, 300))
	}
	return body, nil
}

func doGet(ctx context.Context, token string, buildReq func(token string) (*http.Request, error)) ([]byte, int, error) {
	req, err := buildReq(token)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// shellJoin renders args for display/logging, quoting any that contain spaces.
func shellJoin(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		if a == "" || containsSpace(a) {
			out += fmt.Sprintf("%q", a)
		} else {
			out += a
		}
	}
	return out
}

func containsSpace(s string) bool {
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			return true
		}
	}
	return false
}
