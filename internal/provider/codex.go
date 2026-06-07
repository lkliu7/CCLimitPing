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
	"github.com/wavever/CCLimitPing/internal/pricing"
	"github.com/wavever/CCLimitPing/internal/usage"
)

const codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// Codex reads usage via the ChatGPT backend usage endpoint and triggers windows
// via the `codex exec` headless CLI.
type Codex struct {
	cfg  config.ProviderConfig
	auth *auth.CodexAuth
}

func NewCodex(cfg config.ProviderConfig) *Codex {
	return &Codex{cfg: cfg, auth: auth.NewCodexAuth()}
}

func (c *Codex) Name() string { return "codex" }

func (c *Codex) ActiveTask(ctx context.Context) (string, bool, error) {
	return activeCLIProcess(ctx,
		[]string{"codex"},
		[]string{"@openai/codex"})
}

type codexWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int     `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type codexUsageResp struct {
	PlanType  string `json:"plan_type"`
	RateLimit struct {
		Allowed      bool        `json:"allowed"`
		LimitReached bool        `json:"limit_reached"`
		Primary      codexWindow `json:"primary_window"`
		Secondary    codexWindow `json:"secondary_window"`
	} `json:"rate_limit"`
	Credits *struct {
		HasCredits bool   `json:"has_credits"`
		Unlimited  bool   `json:"unlimited"`
		Balance    string `json:"balance"`
	} `json:"credits"`
}

func (c *Codex) ReadUsage(ctx context.Context) (*usage.Usage, error) {
	accountID, _ := c.auth.AccountID(ctx)
	body, err := fetchWithAuth(ctx, c.auth, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if accountID != "" {
			req.Header.Set("chatgpt-account-id", accountID)
		}
		return req, nil
	})
	if err != nil {
		return nil, err
	}

	var r codexUsageResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("codex usage: parsing response: %w", err)
	}

	u := &usage.Usage{
		Provider:     "codex",
		Plan:         r.PlanType,
		FetchedAt:    time.Now(),
		Raw:          body,
		LimitReached: r.RateLimit.LimitReached,
		FiveHour:     codexWindowToUsage(r.RateLimit.Primary),
		Weekly:       codexWindowToUsage(r.RateLimit.Secondary),
	}
	if r.Credits != nil {
		u.Credits = &usage.Credits{
			HasCredits: r.Credits.HasCredits,
			Unlimited:  r.Credits.Unlimited,
			Balance:    r.Credits.Balance,
		}
	}
	return u, nil
}

func codexWindowToUsage(w codexWindow) usage.Window {
	var resetsAt time.Time
	if w.ResetAt > 0 {
		resetsAt = time.Unix(w.ResetAt, 0)
	}
	return usage.Window{
		UsedPercent:   w.UsedPercent,
		ResetsAt:      resetsAt,
		WindowSeconds: w.LimitWindowSeconds,
	}
}

func (c *Codex) Trigger(ctx context.Context, dryRun bool) (*TriggerResult, error) {
	prompt := c.cfg.Prompt
	if prompt == "" {
		prompt = "ok"
	}
	args := []string{"exec", "--skip-git-repo-check", "--json"}
	if c.cfg.ReasoningEffort != "" {
		args = append(args, "-c", "model_reasoning_effort="+c.cfg.ReasoningEffort)
	}
	if c.cfg.Model != "" {
		args = append(args, "-m", c.cfg.Model)
	}
	args = append(args, c.cfg.ExtraArgs...)
	args = append(args, prompt)
	res := &TriggerResult{Command: "codex " + shellJoin(args)}
	if dryRun {
		return res, nil
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return res, fmt.Errorf("codex exec failed: %w: %s", err, truncate(append(stderr.Bytes(), stdout.Bytes()...), 300))
	}

	// codex exec --json emits JSONL; the final `turn.completed` event carries
	// the turn's token usage. output_tokens already includes reasoning tokens,
	// so we don't add reasoning_output_tokens again.
	var cached int
	for _, line := range bytes.Split(stdout.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens       int `json:"input_tokens"`
				CachedInputTokens int `json:"cached_input_tokens"`
				OutputTokens      int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(line, &ev); err != nil || ev.Type != "turn.completed" || ev.Usage == nil {
			continue
		}
		res.InputTokens = ev.Usage.InputTokens
		res.OutputTokens = ev.Usage.OutputTokens
		res.TotalTokens = res.InputTokens + res.OutputTokens
		res.HasUsage = true
		cached = ev.Usage.CachedInputTokens
	}

	// Codex doesn't report a USD cost; derive it from LiteLLM rates like
	// CodexBar/ccusage do.
	if res.HasUsage && c.cfg.Model != "" {
		pctx, pcancel := context.WithTimeout(context.Background(), 10*time.Second)
		if price, ok := pricing.Default().Lookup(pctx, c.cfg.Model); ok {
			res.CostUSD = price.Cost(res.InputTokens, cached, res.OutputTokens)
		}
		pcancel()
	}
	return res, nil
}
