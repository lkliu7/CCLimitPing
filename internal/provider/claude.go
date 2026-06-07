package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/wavever/CCLimitPing/internal/auth"
	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/usage"
)

const (
	claudeUsageURL    = "https://api.anthropic.com/api/oauth/usage"
	claudeOAuthBeta   = "oauth-2025-04-20"
	claudeFiveHourSec = 5 * 60 * 60
	claudeWeeklySec   = 7 * 24 * 60 * 60
)

// Claude reads usage via the OAuth usage endpoint and triggers windows via the
// `claude -p` headless CLI.
type Claude struct {
	cfg  config.ProviderConfig
	auth *auth.ClaudeAuth
}

func NewClaude(cfg config.ProviderConfig) *Claude {
	return &Claude{cfg: cfg, auth: auth.NewClaudeAuth()}
}

func (c *Claude) Name() string { return "claude" }

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
	args := []string{"-p", prompt}
	if c.cfg.Model != "" {
		args = append(args, "--model", c.cfg.Model)
	}
	args = append(args, "--output-format", "json")
	args = append(args, c.cfg.ExtraArgs...)
	res := &TriggerResult{Command: "claude " + shellJoin(args)}
	if dryRun {
		return res, nil
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return res, fmt.Errorf("claude -p failed: %w: %s", err, truncate(append(stderr.Bytes(), stdout.Bytes()...), 300))
	}

	var r struct {
		IsError      bool    `json:"is_error"`
		Result       string  `json:"result"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		Usage        struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &r); err == nil {
		if r.IsError {
			return res, fmt.Errorf("claude -p returned an error: %s", r.Result)
		}
		res.InputTokens = r.Usage.InputTokens + r.Usage.CacheCreationInputTokens + r.Usage.CacheReadInputTokens
		res.OutputTokens = r.Usage.OutputTokens
		res.TotalTokens = res.InputTokens + res.OutputTokens
		res.CostUSD = r.TotalCostUSD
		res.HasUsage = true
	}
	return res, nil
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
