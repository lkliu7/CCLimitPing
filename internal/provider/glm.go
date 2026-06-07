package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wavever/CCLimitPing/internal/auth"
	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/usage"
)

const (
	glmZaiUsageURL = "https://api.z.ai/api/monitor/usage/quota/limit"
	glmZaiChatURL  = "https://api.z.ai/api/coding/paas/v4/chat/completions"
	glmCnUsageURL  = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"
	glmCnChatURL   = "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions"

	glmFiveHourSec = 5 * 60 * 60
	glmWeeklySec   = 7 * 24 * 60 * 60

	glmDefaultModel = "glm-4.6"
)

// GLM (Zhipu / Z.ai GLM Coding Plan) reads usage via the zero-quota monitor
// endpoint and triggers a window with a minimal chat completion. GLM has no
// standalone official CLI — it is consumed through other tools — so the trigger
// is a direct, tiny API call rather than a shell-out.
type GLM struct {
	cfg      config.ProviderConfig
	auth     *auth.GLMAuth
	platform string
	usageURL string
	chatURL  string
}

func NewGLM(cfg config.ProviderConfig) *GLM {
	platform := glmPlatform(cfg.Platform)
	usageURL, chatURL := glmZaiUsageURL, glmZaiChatURL
	if platform == "cn" {
		usageURL, chatURL = glmCnUsageURL, glmCnChatURL
	}
	return &GLM{
		cfg:      cfg,
		auth:     auth.NewGLMAuth(cfg.APIKey, platform),
		platform: platform,
		usageURL: usageURL,
		chatURL:  chatURL,
	}
}

func (g *GLM) Name() string { return "glm" }

// glmPlatform normalizes the configured platform to "global" or "cn".
func glmPlatform(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "cn", "zhipu", "zhipuai", "bigmodel":
		return "cn"
	default:
		return "global"
	}
}

type glmLimit struct {
	Type          string  `json:"type"`          // "TOKENS_LIMIT" / "TIME_LIMIT"
	Unit          int     `json:"unit"`          // (unit,number): (3,5)=5h, (6,1)=weekly
	Number        int     `json:"number"`        //
	Percentage    float64 `json:"percentage"`    // used, 0..100
	Total         float64 `json:"total"`         // token allowance
	NextResetTime int64   `json:"nextResetTime"` // unix milliseconds
}

type glmUsageResp struct {
	Data struct {
		Level  string     `json:"level"`
		Limits []glmLimit `json:"limits"`
	} `json:"data"`
	Limits []glmLimit `json:"limits"` // fallback if the body isn't wrapped in "data"
}

func (g *GLM) ReadUsage(ctx context.Context) (*usage.Usage, error) {
	body, err := fetchWithAuth(ctx, g.auth, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.usageURL, nil)
		if err != nil {
			return nil, err
		}
		// The monitor endpoint takes the API key directly, with NO "Bearer" prefix.
		req.Header.Set("Authorization", token)
		req.Header.Set("Accept-Language", "en-US,en")
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, err
	}

	var r glmUsageResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("glm usage: parsing response: %w", err)
	}
	limits := r.Data.Limits
	if len(limits) == 0 {
		limits = r.Limits
	}

	u := &usage.Usage{
		Provider:  "glm",
		Plan:      r.Data.Level,
		FetchedAt: time.Now(),
		Raw:       body,
	}
	for _, l := range limits {
		if !strings.EqualFold(l.Type, "TOKENS_LIMIT") {
			continue // skip TIME_LIMIT (monthly MCP quota) etc.
		}
		w := usage.Window{UsedPercent: l.Percentage}
		if l.NextResetTime > 0 {
			w.ResetsAt = time.UnixMilli(l.NextResetTime)
		}
		switch {
		case l.Unit == 3 && l.Number == 5:
			w.WindowSeconds = glmFiveHourSec
			u.FiveHour = w
		case l.Unit == 6 && l.Number == 1:
			w.WindowSeconds = glmWeeklySec
			u.Weekly = w
		}
	}
	u.LimitReached = u.FiveHour.UsedPercent >= 100 || u.Weekly.UsedPercent >= 100
	return u, nil
}

type glmChatReq struct {
	Model     string   `json:"model"`
	Messages  []glmMsg `json:"messages"`
	MaxTokens int      `json:"max_tokens"`
}

type glmMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (g *GLM) Trigger(ctx context.Context, dryRun bool) (*TriggerResult, error) {
	prompt := g.cfg.Prompt
	if prompt == "" {
		prompt = "ok"
	}
	model := g.cfg.Model
	if model == "" {
		model = glmDefaultModel
	}

	reqBody, err := json.Marshal(glmChatReq{
		Model:     model,
		Messages:  []glmMsg{{Role: "user", Content: prompt}},
		MaxTokens: 1,
	})
	if err != nil {
		return nil, err
	}
	// Display a runnable-looking curl with the key redacted; the payload shown
	// is exactly what we send.
	res := &TriggerResult{Command: fmt.Sprintf(
		"curl -s %s -H 'Authorization: Bearer ***' -d '%s'", g.chatURL, string(reqBody))}
	if dryRun {
		return res, nil
	}

	token, err := g.auth.Token(ctx)
	if err != nil {
		return res, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.chatURL, bytes.NewReader(reqBody))
	if err != nil {
		return res, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return res, fmt.Errorf("glm chat request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return res, fmt.Errorf("glm chat returned HTTP %d: %s", resp.StatusCode, truncate(respBody, 300))
	}

	var r struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &r); err == nil {
		if r.Error != nil && r.Error.Message != "" {
			return res, fmt.Errorf("glm chat error: %s", r.Error.Message)
		}
		res.InputTokens = r.Usage.PromptTokens
		res.OutputTokens = r.Usage.CompletionTokens
		res.TotalTokens = r.Usage.TotalTokens
		if res.TotalTokens == 0 {
			res.TotalTokens = res.InputTokens + res.OutputTokens
		}
		res.HasUsage = res.TotalTokens > 0
	}
	// GLM is a subscription (per-prompt quota), so there's no per-call USD cost
	// to report; CostUSD stays 0 and only the token count is shown.
	return res, nil
}
