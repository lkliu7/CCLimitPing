package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wavever/CCLimitPing/internal/config"
)

func TestClaudeInteractiveArgsDropsPrintOnlyFlags(t *testing.T) {
	got := claudeInteractiveArgs([]string{
		"--max-turns", "1",
		"--output-format=json",
		"--tools", "Read",
		"--bare",
		"--permission-mode", "plan",
		"--json-schema", "{}",
	})
	want := []string{"--tools", "Read", "--permission-mode", "plan"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interactive args = %#v, want %#v", got, want)
	}
}

func TestClaudeTriggerDryRunUsesInteractiveCommand(t *testing.T) {
	c := NewClaude(config.ProviderConfig{
		Prompt: ".",
		Model:  "haiku",
		ExtraArgs: []string{
			"--max-turns", "1",
			"--output-format", "json",
		},
	})

	res, err := c.Trigger(context.Background(), true)
	if err != nil {
		t.Fatalf("dry-run trigger: %v", err)
	}
	if res.Command != "claude --model haiku ." {
		t.Fatalf("command = %q, want %q", res.Command, "claude --model haiku .")
	}
	if strings.Contains(res.Command, " -p") || strings.Contains(res.Command, "--print") {
		t.Fatalf("command still uses headless mode: %q", res.Command)
	}
}

func TestNormalizedClaudeVersion(t *testing.T) {
	if got := normalizedClaudeVersion("2.1.168 (Claude Code)\n"); got != "2.1.168" {
		t.Fatalf("version = %q, want %q", got, "2.1.168")
	}
	if got := normalizedClaudeVersion(" \n\t"); got != "" {
		t.Fatalf("empty version = %q, want empty", got)
	}
}
