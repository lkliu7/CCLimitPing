package scheduler

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// heartbeatFrames is an ASCII ECG-style pulse that shows the watcher is alive
// while it sleeps between pings. Keep every frame the same width so provider
// text does not shift as the animation advances.
var heartbeatFrames = []string{
	"____/\\___",
	"_____/\\__",
	"______/\\_",
	"_______/\\",
	"_________",
	"/\\_______",
	"_/\\______",
	"__/\\_____",
	"___/\\____",
}

const (
	heartbeatFrameWidth = 9
	liveTick            = 250 * time.Millisecond
	ansiDim             = "\033[2m"
	ansiCyan            = "\033[36m"
	ansiReset           = "\033[0m"
	eraseLine           = "\r\033[K" // carriage return + clear to end of line
)

// liveStatus renders a single self-updating line at the bottom of the terminal:
// a heartbeat plus each provider's current state and a live countdown to its next
// action. Log lines written through it (it implements io.Writer for the
// scheduler's logger) scroll above the status line, which redraws beneath them.
//
// When the output isn't an interactive terminal (e.g. piped to a file by `bg`),
// liveStatus is a transparent pass-through: Write just forwards to out and no
// status line is drawn, so log files stay free of ANSI control sequences.
type liveStatus struct {
	out     io.Writer
	enabled bool
	color   bool

	mu    sync.Mutex
	items map[string]liveItem
	order []string // stable display order, one entry per target
	frame int
	drawn bool
}

// liveItem is one provider's current state on the status line.
type liveItem struct {
	state    string    // short description, e.g. "5h window 12%"
	deadline time.Time // zero = no countdown shown
}

func newLiveStatus(out io.Writer, names []string) *liveStatus {
	enabled := isTerminalWriter(out) && os.Getenv("TERM") != "dumb"
	return &liveStatus{
		out:     out,
		enabled: enabled,
		color:   enabled && os.Getenv("NO_COLOR") == "",
		items:   make(map[string]liveItem, len(names)),
		order:   append([]string(nil), names...),
	}
}

// set updates a provider's state and optional countdown deadline. Safe to call
// from any target goroutine; a no-op visually when the status line is disabled.
func (l *liveStatus) set(name, state string, deadline time.Time) {
	l.mu.Lock()
	l.items[name] = liveItem{state: state, deadline: deadline}
	l.mu.Unlock()
}

// Write implements io.Writer for the scheduler's logger: it erases the status
// line, emits the log output, then redraws the status line beneath it.
func (l *liveStatus) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.enabled && l.drawn {
		io.WriteString(l.out, eraseLine)
		l.drawn = false
	}
	n, err := l.out.Write(p)
	if err != nil {
		return n, err
	}
	l.drawLocked()
	return n, nil
}

// run drives the heartbeat/countdown until ctx is cancelled, then clears the line.
func (l *liveStatus) run(ctx context.Context) {
	if !l.enabled {
		return
	}
	t := time.NewTicker(liveTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			l.clear()
			return
		case <-t.C:
			l.tick()
		}
	}
}

func (l *liveStatus) tick() {
	l.mu.Lock()
	l.frame++
	l.drawLocked()
	l.mu.Unlock()
}

func (l *liveStatus) clear() {
	l.mu.Lock()
	if l.enabled && l.drawn {
		io.WriteString(l.out, eraseLine)
		l.drawn = false
	}
	l.mu.Unlock()
}

// drawLocked renders the status line. The caller must hold l.mu.
func (l *liveStatus) drawLocked() {
	if !l.enabled {
		return
	}
	plain := l.renderLocked()
	if w := terminalWidth(l.out); w > 0 {
		plain = truncateRunes(plain, w-1) // leave a column so the cursor never wraps
	}
	io.WriteString(l.out, eraseLine+l.colorize(plain))
	l.drawn = true
}

// renderLocked builds the plain (ANSI-free) status line. The caller must hold l.mu.
func (l *liveStatus) renderLocked() string {
	heartbeat := heartbeatFrames[l.frame%len(heartbeatFrames)]
	parts := make([]string, 0, len(l.order))
	for _, name := range l.order {
		it, ok := l.items[name]
		if !ok || it.state == "" {
			continue
		}
		s := name + ": " + it.state
		if !it.deadline.IsZero() {
			s += " (in " + humanCountdown(time.Until(it.deadline)) + ")"
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return heartbeat + " watching…"
	}
	return heartbeat + " " + strings.Join(parts, "  ·  ")
}

// colorize tints the ASCII heartbeat cyan and dims the rest. A no-op when color
// is disabled.
func (l *liveStatus) colorize(plain string) string {
	if !l.color {
		return plain
	}
	r := []rune(plain)
	if len(r) == 0 {
		return plain
	}
	accentRunes := heartbeatFrameWidth
	if len(r) < accentRunes {
		accentRunes = len(r)
	}
	return ansiCyan + string(r[:accentRunes]) + ansiReset + ansiDim + string(r[accentRunes:]) + ansiReset
}

// humanCountdown formats a duration compactly, dropping seconds when far out so
// distant countdowns don't churn the line every tick.
func humanCountdown(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, h)
	case h > 0:
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// truncateRunes shortens s to at most max runes, appending an ellipsis. Counting
// runes (not bytes) keeps the multibyte spinner/middots from miscounting width.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return string(r[:1])
	}
	return string(r[:max-1]) + "…"
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// terminalWidth returns the column count for out, or 0 if it can't be determined
// (in which case the status line is drawn untruncated).
func terminalWidth(out io.Writer) int {
	f, ok := out.(*os.File)
	if !ok {
		return 0
	}
	_, cols, err := pty.Getsize(f)
	if err != nil {
		return 0
	}
	return cols
}
