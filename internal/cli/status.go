package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/usage"
)

func newStatusCmd() *cobra.Command {
	var verbose bool
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
			out := cmd.OutOrStdout()
			failed := 0
			for _, p := range providers {
				ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
				u, err := p.ReadUsage(ctx)
				cancel()
				if err != nil {
					fmt.Fprintf(out, "%-7s  error: %v\n", p.Name(), err)
					failed++
					continue
				}
				printUsage(cmd, u, verbose)
			}
			if failed > 0 {
				return fmt.Errorf("status failed for %d provider(s)", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, text.statusVerboseFlag)
	return cmd
}

func printUsage(cmd *cobra.Command, u *usage.Usage, verbose bool) {
	out := cmd.OutOrStdout()
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
