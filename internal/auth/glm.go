package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// GLMAuth resolves the Zhipu / Z.ai GLM Coding Plan API key. Unlike Claude and
// Codex (OAuth), GLM authenticates with a long-lived API key, so there is no
// token refresh: the key comes from config or an environment variable.
type GLMAuth struct {
	mu        sync.Mutex
	configKey string
	platform  string // "global" (api.z.ai) or "cn" (open.bigmodel.cn)
	key       string
}

// NewGLMAuth builds a GLMAuth. platform is expected to be normalized to
// "global" or "cn" already; anything other than "cn" is treated as global.
func NewGLMAuth(configKey, platform string) *GLMAuth {
	return &GLMAuth{configKey: configKey, platform: platform}
}

// Token returns the cached API key, resolving it from config/env on first use.
func (a *GLMAuth) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.key != "" {
		return a.key, nil
	}
	k, err := a.resolveLocked()
	if err != nil {
		return "", err
	}
	a.key = k
	return k, nil
}

// Reload re-reads the key from config/env (the user may have just set it).
func (a *GLMAuth) Reload(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	k, err := a.resolveLocked()
	if err != nil {
		return "", err
	}
	a.key = k
	return k, nil
}

// Refresh has no meaning for a static API key; a 401 means the key is wrong.
func (a *GLMAuth) Refresh(ctx context.Context) (string, error) {
	return "", fmt.Errorf("glm: API key rejected; update [glm].api_key or its env var (GLM uses a static key, there is no refresh)")
}

func (a *GLMAuth) resolveLocked() (string, error) {
	if a.configKey != "" {
		return a.configKey, nil
	}
	var envs []string
	if a.platform == "cn" {
		envs = []string{"ZHIPU_API_KEY", "ZHIPUAI_API_KEY", "GLM_API_KEY"}
	} else {
		envs = []string{"ZAI_API_KEY", "GLM_API_KEY"}
	}
	for _, e := range envs {
		if v := strings.TrimSpace(os.Getenv(e)); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("glm: no API key found (set [glm].api_key or $%s)", strings.Join(envs, " / $"))
}
