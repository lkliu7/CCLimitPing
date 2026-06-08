package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/wavever/CCLimitPing/internal/activity"
	"github.com/wavever/CCLimitPing/internal/auth"
	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/pricing"
	"github.com/wavever/CCLimitPing/internal/usage"
)

const (
	codexDefaultBaseURL = "https://chatgpt.com/backend-api"
	codexChatGPTPath    = "/wham/usage"
	codexAPIPath        = "/api/codex/usage"
	codexUserAgent      = "limitping"
)

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

func (c *Codex) ActiveTask(_ context.Context) (string, bool, error) {
	// Active-session detection relies entirely on the CLI hooks (see `limitping
	// hooks install`). Without them we don't guess from the process list — the
	// scheduler just pings.
	if !activity.Enabled("codex") {
		return "", false, nil
	}
	return activity.Active("codex")
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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", codexUserAgent)
		if accountID != "" {
			req.Header.Set("ChatGPT-Account-Id", accountID)
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

func codexUsageURL() string {
	base := codexDefaultBaseURL
	if contents, err := os.ReadFile(codexConfigPath()); err == nil {
		if configured := parseCodexBaseURL(string(contents)); configured != "" {
			base = configured
		}
	}
	return codexUsageURLFromBase(base)
}

func codexUsageURLFromBase(base string) string {
	normalized := normalizeCodexBaseURL(base)
	path := codexAPIPath
	if strings.Contains(normalized, "/backend-api") {
		path = codexChatGPTPath
	}
	endpoint := normalized + path
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return codexDefaultBaseURL + codexChatGPTPath
	}
	return endpoint
}

func normalizeCodexBaseURL(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = codexDefaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	if (strings.HasPrefix(base, "https://chatgpt.com") || strings.HasPrefix(base, "https://chat.openai.com")) &&
		!strings.Contains(base, "/backend-api") {
		base += "/backend-api"
	}
	return base
}

func parseCodexBaseURL(contents string) string {
	var cfg struct {
		ChatGPTBaseURL string `toml:"chatgpt_base_url"`
	}
	if _, err := toml.Decode(contents, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.ChatGPTBaseURL)
}

func codexConfigPath() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return filepath.Join(h, "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".codex", "config.toml")
	}
	return filepath.Join(home, ".codex", "config.toml")
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
