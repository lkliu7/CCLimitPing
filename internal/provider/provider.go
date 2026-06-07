// Package provider implements per-provider usage reading (zero-quota, via the
// OAuth usage endpoints) and window triggering (via the official CLIs).
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/wavever/CCLimitPing/internal/usage"
)

const (
	usageGETAttempts = 3
	usageGETBackoff  = 500 * time.Millisecond
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

// ActiveTaskDetector is optionally implemented by providers that can tell
// whether a user-owned local task is already running and likely to start the
// next window itself.
type ActiveTaskDetector interface {
	ActiveTask(ctx context.Context) (description string, active bool, err error)
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
	var lastBody []byte
	var lastStatus int
	var lastErr error

	for attempt := 1; attempt <= usageGETAttempts; attempt++ {
		req, err := buildReq(token)
		if err != nil {
			return nil, 0, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			if !shouldRetryUsageGET(ctx, attempt, 0, err) {
				return nil, 0, err
			}
			if !sleepBeforeUsageRetry(ctx, attempt) {
				return nil, 0, ctx.Err()
			}
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		lastBody, lastStatus, lastErr = body, resp.StatusCode, readErr

		if readErr != nil {
			if !shouldRetryUsageGET(ctx, attempt, resp.StatusCode, readErr) {
				return nil, resp.StatusCode, readErr
			}
			if !sleepBeforeUsageRetry(ctx, attempt) {
				return nil, 0, ctx.Err()
			}
			continue
		}
		if !retryableHTTPStatus(resp.StatusCode) || !shouldRetryUsageGET(ctx, attempt, resp.StatusCode, nil) {
			return body, resp.StatusCode, nil
		}
		if !sleepBeforeUsageRetry(ctx, attempt) {
			return nil, 0, ctx.Err()
		}
	}

	return lastBody, lastStatus, lastErr
}

func shouldRetryUsageGET(ctx context.Context, attempt, status int, err error) bool {
	if attempt >= usageGETAttempts || ctx.Err() != nil {
		return false
	}
	if err != nil {
		return transientNetError(err)
	}
	return retryableHTTPStatus(status)
}

func transientNetError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary())
}

func retryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return status >= 500 && status != http.StatusNotImplemented
	}
}

func sleepBeforeUsageRetry(ctx context.Context, attempt int) bool {
	delay := usageGETBackoff * time.Duration(1<<(attempt-1))
	jitter := time.Duration(rand.Int63n(int64(delay / 4)))
	timer := time.NewTimer(delay + jitter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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
