// Package notify sends desktop notifications. On macOS it uses osascript; on
// other platforms it is a no-op so callers don't need to branch.
package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

// Enabled reports whether notifications can actually be delivered here.
func Enabled() bool { return runtime.GOOS == "darwin" }

// Notify shows a desktop notification with the given title and message.
// Failures are silently ignored — a missed banner must never break the daemon.
func Notify(title, message string) {
	if runtime.GOOS != "darwin" {
		return
	}
	script := "display notification " + quote(message) + " with title " + quote(title)
	_ = exec.Command("osascript", "-e", script).Run()
}

// quote escapes a string for embedding in an AppleScript string literal.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
