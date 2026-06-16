package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/provider"
	"github.com/wavever/CCLimitPing/internal/usage"
)

func newStatusCmd() *cobra.Command {
	var verbose bool
	var jsonOut bool
	text := localizedText()
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"s", "stat"},
		Short:   text.statusShort,
		Long:    text.statusLong,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			providers := enabledProviders(cfg)
			if len(providers) == 0 {
				return fmt.Errorf("no providers enabled in config")
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), text, providers, verbose, jsonOut)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, text.statusVerboseFlag)
	cmd.Flags().BoolVar(&jsonOut, "json", false, text.statusJSONFlag)
	return cmd
}

func runStatus(ctx context.Context, out, progress io.Writer, text cliText, providers []provider.Provider, verbose, jsonOut bool) error {
	if progress == nil {
		progress = io.Discard
	}
	// In JSON mode keep stdout a single valid document: suppress the
	// "Fetching..." progress chatter that would otherwise interleave.
	if jsonOut {
		progress = io.Discard
	}
	failed := 0
	entries := make([]statusJSON, 0, len(providers))
	for _, p := range providers {
		if text.statusFetchingFmt != "" {
			fmt.Fprintf(progress, text.statusFetchingFmt, p.Name())
		}
		readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		u, err := p.ReadUsage(readCtx)
		cancel()
		if err != nil {
			failed++
			if jsonOut {
				entries = append(entries, statusJSON{Provider: p.Name(), Error: err.Error()})
				continue
			}
			fmt.Fprintf(out, "%-7s  error: %v\n", p.Name(), err)
			continue
		}
		if jsonOut {
			entries = append(entries, newStatusJSON(u, verbose))
			continue
		}
		printUsage(out, u, verbose)
	}
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entries); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("status failed for %d provider(s)", failed)
	}
	return nil
}

// statusJSON is the stable, documented shape emitted by `status --json`. It is
// decoupled from usage.Usage so the internal model can evolve without breaking
// scripts that consume this output.
type statusJSON struct {
	Provider     string          `json:"provider"`
	Plan         string          `json:"plan,omitempty"`
	FiveHour     *windowJSON     `json:"five_hour,omitempty"`
	Weekly       *windowJSON     `json:"weekly,omitempty"`
	Credits      *creditsJSON    `json:"credits,omitempty"`
	LimitReached bool            `json:"limit_reached"`
	FetchedAt    string          `json:"fetched_at,omitempty"`
	Raw          json.RawMessage `json:"raw,omitempty"`
	Error        string          `json:"error,omitempty"`
}

type windowJSON struct {
	UsedPercent      float64 `json:"used_percent"`
	Active           bool    `json:"active"`
	ResetsAt         string  `json:"resets_at,omitempty"`
	RemainingSeconds int     `json:"remaining_seconds"`
	WindowSeconds    int     `json:"window_seconds,omitempty"`
}

type creditsJSON struct {
	HasCredits bool   `json:"has_credits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance,omitempty"`
}

func newStatusJSON(u *usage.Usage, verbose bool) statusJSON {
	s := statusJSON{
		Provider:     u.Provider,
		Plan:         u.Plan,
		FiveHour:     newWindowJSON(u.FiveHour),
		Weekly:       newWindowJSON(u.Weekly),
		LimitReached: u.LimitReached,
	}
	if !u.FetchedAt.IsZero() {
		s.FetchedAt = u.FetchedAt.Format(time.RFC3339)
	}
	if u.Credits != nil {
		s.Credits = &creditsJSON{
			HasCredits: u.Credits.HasCredits,
			Unlimited:  u.Credits.Unlimited,
			Balance:    u.Credits.Balance,
		}
	}
	if verbose && json.Valid(u.Raw) {
		s.Raw = json.RawMessage(u.Raw)
	}
	return s
}

func newWindowJSON(w usage.Window) *windowJSON {
	j := &windowJSON{
		UsedPercent:      w.UsedPercent,
		Active:           w.Active(),
		RemainingSeconds: int(w.Remaining().Seconds()),
		WindowSeconds:    w.WindowSeconds,
	}
	if !w.ResetsAt.IsZero() {
		j.ResetsAt = w.ResetsAt.Format(time.RFC3339)
	}
	return j
}

func printUsage(out io.Writer, u *usage.Usage, verbose bool) {
	plan := u.Plan
	if plan != "" {
		plan = " (" + plan + ")"
	}
	fmt.Fprintf(out, "%s%s\n", u.Provider, plan)
	fmt.Fprintf(out, "  5h     %s\n", fmtWindow(u.FiveHour))
	fmt.Fprintf(out, "  weekly %s\n", fmtWindow(u.Weekly))
	if u.Credits != nil && (u.Credits.HasCredits || u.Credits.Unlimited) {
		if u.Credits.Unlimited {
			fmt.Fprintf(out, "  credits unlimited\n")
		} else {
			fmt.Fprintf(out, "  credits %s\n", u.Credits.Balance)
		}
	}
	if verbose {
		fmt.Fprintf(out, "  raw: %s\n", string(u.Raw))
	}
	fmt.Fprintln(out)
}

func fmtWindow(w usage.Window) string {
	bar := usageBar(w.UsedPercent)
	if w.ResetsAt.IsZero() {
		return fmt.Sprintf("%s %5.1f%%  (no active window)", bar, w.UsedPercent)
	}
	return fmt.Sprintf("%s %5.1f%%  resets in %-8s (%s)",
		bar, w.UsedPercent, fmtDur(w.Remaining()), w.ResetsAt.Local().Format("Mon 15:04"))
}

func usageBar(pct float64) string {
	const width = 10
	filled := int(pct/100*width + 0.5)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	b := make([]rune, width)
	for i := range b {
		if i < filled {
			b[i] = '█'
		} else {
			b[i] = '░'
		}
	}
	return "[" + string(b) + "]"
}

func fmtDur(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	d = d.Round(time.Minute)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
