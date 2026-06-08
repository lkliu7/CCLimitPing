package provider

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wavever/CCLimitPing/internal/config"
)

func TestCodexReadUsageSendsCompatibleHeaders(t *testing.T) {
	oldClient := usageHTTPClient
	defer func() { usageHTTPClient = oldClient }()

	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	authJSON := `{"tokens":{"access_token":"access-token","refresh_token":"refresh-token","account_id":"account-123"}}`
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(authJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.String(); got != "https://chatgpt.com/backend-api/wham/usage" {
			t.Fatalf("url = %q", got)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := req.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("accept = %q", got)
		}
		if got := req.Header.Get("User-Agent"); got != codexUserAgent {
			t.Fatalf("user-agent = %q", got)
		}
		if got := req.Header.Get("ChatGPT-Account-Id"); got != "account-123" {
			t.Fatalf("account id = %q", got)
		}
		body := `{
			"plan_type": "pro",
			"rate_limit": {
				"limit_reached": false,
				"primary_window": {"used_percent": 12, "limit_window_seconds": 18000, "reset_at": 4102444800},
				"secondary_window": {"used_percent": 34, "limit_window_seconds": 604800, "reset_at": 4103049600}
			}
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	u, err := NewCodex(config.ProviderConfig{}).ReadUsage(context.Background())
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if u.Provider != "codex" || u.Plan != "pro" {
		t.Fatalf("usage = %#v", u)
	}
	if u.FiveHour.UsedPercent != 12 || u.Weekly.UsedPercent != 34 {
		t.Fatalf("windows = %#v %#v", u.FiveHour, u.Weekly)
	}
}

func TestCodexUsageURLFromBase(t *testing.T) {
	cases := map[string]string{
		"":                                 "https://chatgpt.com/backend-api/wham/usage",
		"https://chatgpt.com/backend-api/": "https://chatgpt.com/backend-api/wham/usage",
		"https://chat.openai.com":          "https://chat.openai.com/backend-api/wham/usage",
		"https://api.openai.com":           "https://api.openai.com/api/codex/usage",
		"https://example.test/custom/base": "https://example.test/custom/base/api/codex/usage",
		"://bad":                           "https://chatgpt.com/backend-api/wham/usage",
	}
	for base, want := range cases {
		if got := codexUsageURLFromBase(base); got != want {
			t.Fatalf("codexUsageURLFromBase(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestParseCodexBaseURL(t *testing.T) {
	contents := `
model = "gpt-5.4-mini"
chatgpt_base_url = "https://api.openai.com"
`
	if got := parseCodexBaseURL(contents); got != "https://api.openai.com" {
		t.Fatalf("base url = %q", got)
	}
}
