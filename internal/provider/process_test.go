package provider

import "testing"

func TestProcessMatchesDirectCommand(t *testing.T) {
	proc := processInfo{pid: 123, comm: "/usr/local/bin/claude", args: "claude ."}
	if !processMatches(proc, []string{"claude"}, nil) {
		t.Fatal("expected direct claude process to match")
	}
}

func TestProcessMatchesNodeWrapperMarker(t *testing.T) {
	proc := processInfo{
		pid:  123,
		comm: "/usr/local/bin/node",
		args: "node /opt/npm/lib/node_modules/@anthropic-ai/claude-code/cli.js .",
	}
	if !processMatches(proc, []string{"claude"}, []string{"@anthropic-ai/claude"}) {
		t.Fatal("expected node wrapper process to match marker")
	}
}

func TestProcessBaseNormalizesExe(t *testing.T) {
	if got := processBase(`/usr/local/bin/codex.exe`); got != "codex" {
		t.Fatalf("processBase = %q, want codex", got)
	}
}

func TestParseProcessLine(t *testing.T) {
	proc, ok := parseProcessLine("  123 /usr/bin/node node /path/codex.js")
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if proc.pid != 123 || proc.comm != "/usr/bin/node" || proc.args != "node /path/codex.js" {
		t.Fatalf("parsed process = %#v", proc)
	}
}
