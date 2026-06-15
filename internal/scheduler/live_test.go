package scheduler

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestHumanCountdown(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{0, "0s"},
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m30s"},
		{2*time.Hour + 3*time.Minute + 4*time.Second, "2h03m04s"},
		{49 * time.Hour, "2d1h"},
	}
	for _, c := range cases {
		if got := humanCountdown(c.in); got != c.want {
			t.Errorf("humanCountdown(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 0, "hello"},             // max<=0 means no truncation
		{"hello", 10, "hello"},            // shorter than max
		{"hello", 5, "hello"},             // exactly max
		{"hello", 4, "hel…"},              // truncated with ellipsis
		{"hello", 1, "h"},                 // no room for the ellipsis
		{"pulse · claude", 8, "pulse ·…"}, // counts runes, not bytes
	}
	for _, c := range cases {
		if got := truncateRunes(c.in, c.max); got != c.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestHeartbeatFramesAreFixedWidthASCII(t *testing.T) {
	if len(heartbeatFrames) == 0 {
		t.Fatal("heartbeatFrames must not be empty")
	}
	for _, frame := range heartbeatFrames {
		if got := len([]rune(frame)); got != heartbeatFrameWidth {
			t.Fatalf("heartbeat frame %q width = %d, want %d", frame, got, heartbeatFrameWidth)
		}
		for _, r := range frame {
			if r > 127 {
				t.Fatalf("heartbeat frame %q contains non-ASCII rune %q", frame, r)
			}
		}
	}
}

// When the output isn't a terminal the status line must be a transparent
// pass-through: log bytes go out verbatim with no ANSI control sequences.
func TestLiveStatusDisabledPassthrough(t *testing.T) {
	var buf bytes.Buffer
	l := newLiveStatus(&buf, []string{"claude"})
	if l.enabled {
		t.Fatal("expected liveStatus disabled for a non-terminal writer")
	}
	l.set("claude", "5h window 12%", time.Now().Add(time.Hour))
	l.tick()  // no-op while disabled
	l.clear() // no-op while disabled

	const line = "2026/06/09 [claude] ping sent\n"
	if _, err := l.Write([]byte(line)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := buf.String(); got != line {
		t.Fatalf("disabled Write = %q, want verbatim %q", got, line)
	}
	if strings.ContainsRune(buf.String(), '\r') || strings.Contains(buf.String(), "\033[") {
		t.Fatalf("disabled output leaked control sequences: %q", buf.String())
	}
}

func TestLiveStatusRender(t *testing.T) {
	var buf bytes.Buffer
	// Construct an enabled-but-colorless renderer directly (a real TTY isn't
	// available under test) so we can assert the drawn line.
	l := &liveStatus{
		out:     &buf,
		enabled: true,
		items:   make(map[string]liveItem),
		order:   []string{"claude", "codex"},
	}
	l.set("claude", "5h window 12%", time.Time{})
	l.set("codex", "checking usage…", time.Time{})

	l.mu.Lock()
	l.drawLocked()
	l.mu.Unlock()

	got := buf.String()
	if !strings.HasPrefix(got, eraseLine) {
		t.Errorf("drawn line should start by erasing the line, got %q", got)
	}
	want := "____/\\___ claude: 5h window 12%  ·  codex: checking usage…"
	if !strings.Contains(got, want) {
		t.Errorf("drawn line = %q, want it to contain %q", got, want)
	}
	if !l.drawn {
		t.Error("drawLocked should mark the line as drawn")
	}
}
