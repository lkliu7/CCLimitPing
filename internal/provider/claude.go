package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"

	"github.com/wavever/CCLimitPing/internal/auth"
	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/usage"
)

const (
	claudeUsageURL    = "https://api.anthropic.com/api/oauth/usage"
	claudeOAuthBeta   = "oauth-2025-04-20"
	claudeFiveHourSec = 5 * 60 * 60
	claudeWeeklySec   = 7 * 24 * 60 * 60

	claudeInteractiveExitDelay = 2 * time.Second
)

// Claude reads usage via the OAuth usage endpoint and triggers windows via the
// interactive, TTY-backed Claude Code CLI. Print mode is intentionally avoided
// because it is billed through Agent SDK/API credits rather than Claude
// subscription limits.
type Claude struct {
	cfg  config.ProviderConfig
	auth *auth.ClaudeAuth
}

func NewClaude(cfg config.ProviderConfig) *Claude {
	return &Claude{cfg: cfg, auth: auth.NewClaudeAuth()}
}

func (c *Claude) Name() string { return "claude" }

func (c *Claude) ActiveTask(ctx context.Context) (string, bool, error) {
	return activeCLIProcess(ctx,
		[]string{"claude"},
		[]string{"claude-code", "@anthropic-ai/claude"})
}

type claudeWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

type claudeUsageResp struct {
	FiveHour claudeWindow `json:"five_hour"`
	SevenDay claudeWindow `json:"seven_day"`
}

func (c *Claude) ReadUsage(ctx context.Context) (*usage.Usage, error) {
	body, err := fetchWithAuth(ctx, c.auth, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeUsageURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("anthropic-beta", claudeOAuthBeta)
		return req, nil
	})
	if err != nil {
		return nil, err
	}

	var r claudeUsageResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("claude usage: parsing response: %w", err)
	}

	u := &usage.Usage{
		Provider:  "claude",
		FetchedAt: time.Now(),
		Raw:       body,
		FiveHour: usage.Window{
			UsedPercent:   r.FiveHour.Utilization,
			ResetsAt:      parseTime(r.FiveHour.ResetsAt),
			WindowSeconds: claudeFiveHourSec,
		},
		Weekly: usage.Window{
			UsedPercent:   r.SevenDay.Utilization,
			ResetsAt:      parseTime(r.SevenDay.ResetsAt),
			WindowSeconds: claudeWeeklySec,
		},
	}
	u.LimitReached = u.FiveHour.UsedPercent >= 100 || u.Weekly.UsedPercent >= 100
	return u, nil
}

func (c *Claude) Trigger(ctx context.Context, dryRun bool) (*TriggerResult, error) {
	prompt := c.cfg.Prompt
	if prompt == "" {
		prompt = "."
	}
	args := []string{}
	if c.cfg.Model != "" {
		args = append(args, "--model", c.cfg.Model)
	}
	args = append(args, claudeInteractiveArgs(c.cfg.ExtraArgs)...)
	args = append(args, prompt)

	res := &TriggerResult{Command: "claude " + shellJoin(args)}
	if dryRun {
		return res, nil
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return res, fmt.Errorf("claude interactive failed to start: %w", err)
	}
	defer ptmx.Close()

	output := &limitedBuffer{limit: 4096}
	go func() {
		_, _ = io.Copy(output, ptmx)
	}()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	exitTimer := time.NewTimer(claudeInteractiveExitDelay)
	defer exitTimer.Stop()

	select {
	case err := <-done:
		return res, claudeInteractiveErr(err, output)
	case <-ctx.Done():
		return res, claudeInteractiveCancel(ctx, cmd, ptmx, done, output)
	case <-exitTimer.C:
		if _, err := ptmx.Write([]byte("/exit\r")); err != nil {
			return res, fmt.Errorf("claude interactive failed to queue exit: %w: %s", err, truncate(output.Bytes(), 300))
		}
	}

	select {
	case err := <-done:
		return res, claudeInteractiveErr(err, output)
	case <-ctx.Done():
		return res, claudeInteractiveCancel(ctx, cmd, ptmx, done, output)
	}
}

func claudeInteractiveErr(err error, output *limitedBuffer) error {
	if err == nil {
		return nil
	}
	tail := truncate(output.Bytes(), 300)
	if tail == "" {
		return fmt.Errorf("claude interactive failed: %w", err)
	}
	return fmt.Errorf("claude interactive failed: %w: %s", err, tail)
}

func claudeInteractiveCancel(ctx context.Context, cmd *exec.Cmd, ptmx *os.File, done <-chan error, output *limitedBuffer) error {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = ptmx.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
	tail := truncate(output.Bytes(), 300)
	if tail == "" {
		return fmt.Errorf("claude interactive cancelled: %w", ctx.Err())
	}
	return fmt.Errorf("claude interactive cancelled: %w: %s", ctx.Err(), tail)
}

func claudeInteractiveArgs(extra []string) []string {
	out := make([]string, 0, len(extra))
	for i := 0; i < len(extra); i++ {
		arg := extra[i]
		flag, inlineValue := splitFlagValue(arg)
		if claudeInteractiveUnsupportedValueArg(flag) {
			if !inlineValue && i+1 < len(extra) {
				i++
			}
			continue
		}
		if claudeInteractiveUnsupportedArg(flag) {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func splitFlagValue(arg string) (flag string, inlineValue bool) {
	if strings.HasPrefix(arg, "--") {
		if i := strings.Index(arg, "="); i > 0 {
			return arg[:i], true
		}
	}
	return arg, false
}

func claudeInteractiveUnsupportedArg(flag string) bool {
	switch flag {
	case "-p", "--print", "--bare", "--init", "--maintenance", "--include-hook-events",
		"--include-partial-messages", "--replay-user-messages", "--prompt-suggestions",
		"--no-session-persistence":
		return true
	default:
		return false
	}
}

func claudeInteractiveUnsupportedValueArg(flag string) bool {
	switch flag {
	case "--output-format", "--input-format", "--json-schema", "--max-turns",
		"--max-budget-usd", "--permission-prompt-tool", "--fallback-model":
		return true
	default:
		return false
	}
}

type limitedBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if b.limit > 0 && len(b.buf) > b.limit {
		b.buf = b.buf[len(b.buf)-b.limit:]
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf...)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}
