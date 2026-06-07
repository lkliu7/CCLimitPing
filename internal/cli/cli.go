// Package cli wires up the limitping command-line interface.
package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/provider"
	"github.com/wavever/CCLimitPing/internal/scheduler"
)

// Version is the binary version, overridable at build time via -ldflags.
var Version = "0.1.0"

// Execute runs the root command.
func Execute() error {
	root := &cobra.Command{
		Use:           "limitping",
		Short:         "Keep Claude Code / Codex rate-limit windows back-to-back",
		Long:          "limitping pings your AI coding provider the moment its 5h rate-limit window resets, so the next window starts immediately and stays aligned. Usage is read via zero-quota endpoints; pings go through the official CLIs.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newStatusCmd(), newPingCmd(), newWatchCmd(), newConfigCmd(), newVersionCmd())
	return root.Execute()
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "limitping %s\n", Version)
		},
	}
}

// buildProvider constructs a single provider from config.
func buildProvider(name string, cfg config.Config) (provider.Provider, error) {
	switch name {
	case "claude":
		return provider.NewClaude(cfg.Claude), nil
	case "codex":
		return provider.NewCodex(cfg.Codex), nil
	case "glm":
		return provider.NewGLM(cfg.GLM), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (want claude, codex, glm, or all)", name)
	}
}

// enabledProviders returns the providers marked enabled in config.
func enabledProviders(cfg config.Config) []provider.Provider {
	var ps []provider.Provider
	if cfg.Claude.Enabled {
		ps = append(ps, provider.NewClaude(cfg.Claude))
	}
	if cfg.Codex.Enabled {
		ps = append(ps, provider.NewCodex(cfg.Codex))
	}
	if cfg.GLM.Enabled {
		ps = append(ps, provider.NewGLM(cfg.GLM))
	}
	return ps
}

// selectProviders resolves the --provider flag value to a provider set. "all"
// (or empty) returns the enabled providers; a specific name returns that one
// even if disabled (explicit override).
func selectProviders(cfg config.Config, name string) ([]provider.Provider, error) {
	if name == "" || name == "all" {
		ps := enabledProviders(cfg)
		if len(ps) == 0 {
			return nil, fmt.Errorf("no providers enabled in config")
		}
		return ps, nil
	}
	p, err := buildProvider(name, cfg)
	if err != nil {
		return nil, err
	}
	return []provider.Provider{p}, nil
}

// buildTargets builds scheduler targets for all enabled providers, parsing each
// provider's align_start anchor.
func buildTargets(cfg config.Config) ([]scheduler.Target, error) {
	var targets []scheduler.Target
	add := func(p provider.Provider, alignStart string) error {
		var anchor time.Time
		if alignStart != "" {
			t, err := time.Parse(time.RFC3339, alignStart)
			if err != nil {
				return fmt.Errorf("%s align_start: %w", p.Name(), err)
			}
			anchor = t
		}
		targets = append(targets, scheduler.Target{Provider: p, AlignStart: anchor})
		return nil
	}
	if cfg.Claude.Enabled {
		if err := add(provider.NewClaude(cfg.Claude), cfg.Claude.AlignStart); err != nil {
			return nil, err
		}
	}
	if cfg.Codex.Enabled {
		if err := add(provider.NewCodex(cfg.Codex), cfg.Codex.AlignStart); err != nil {
			return nil, err
		}
	}
	if cfg.GLM.Enabled {
		if err := add(provider.NewGLM(cfg.GLM), cfg.GLM.AlignStart); err != nil {
			return nil, err
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no providers enabled in config")
	}
	return targets, nil
}
