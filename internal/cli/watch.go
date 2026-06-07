package cli

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/scheduler"
)

func newWatchCmd() *cobra.Command {
	var dryRun bool
	text := localizedText()
	cmd := &cobra.Command{
		Use:       "watch [provider]",
		Aliases:   []string{"w"},
		Short:     text.watchShort,
		Long:      text.watchLong,
		Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"claude", "codex", "glm", "all"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			name := "all"
			if len(args) > 0 {
				name = args[0]
			}
			targets, err := selectTargets(cfg, name)
			if err != nil {
				return err
			}

			logger := log.New(cmd.OutOrStdout(), "", log.LstdFlags)
			names := make([]string, len(targets))
			for i, t := range targets {
				names[i] = t.Provider.Name()
			}
			logger.Printf("watching %v (weekly_threshold=%.2f, reset_buffer=%s, notify=%t, dry_run=%t)",
				names, cfg.WeeklyThreshold, cfg.ResetBuffer.Duration, cfg.Notify, dryRun)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			s := scheduler.New(cfg, targets, dryRun, logger)
			s.Run(ctx)
			logger.Printf("shutting down")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, text.watchDryRunFlag)
	return cmd
}
