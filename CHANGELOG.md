# Changelog

All notable changes to this project should be documented here.

This project uses version tags such as `v0.2.0`. Release binaries are published
through GitHub Actions and GoReleaser.

## Unreleased

- Nothing yet.

## v0.4.0

- Added hook-based active-session detection: `limitping hooks install` /
  `uninstall` registers limitping's hooks in `~/.claude/settings.json` and
  `~/.codex/hooks.json` so `watch` can tell when a session is genuinely mid-turn
  (between a prompt and its `Stop`) rather than just having a live process. Falls
  back to the previous process scan when hooks aren't installed.
- Removed the experimental GLM (Zhipu / Z.ai) provider. `limitping` now targets
  Claude Code and Codex only; the `[glm]` config block and `glm` provider
  argument are gone.

## v0.3.0

- Added short command aliases such as `ping` / `p`, `status` / `s`,
  `watch` / `w`, `version` / `v`, `upgrade` / `up`, and `uninstall` / `rm`.
- Updated help output to show command aliases inline and clarify accepted
  `ping` / `watch` provider arguments.
- Added Chinese CLI help text when the system locale is Chinese.
- Documented upgrade, uninstall, and command aliases in the English and Chinese
  READMEs.

## v0.2.1

- `watch` now defers automatic pings while a Claude/Codex CLI task is already
  running, letting that task naturally start the next 5h window.

## v0.2.0

- Switched Claude triggering to the interactive Claude Code CLI so subscription
  window pings keep working after headless print mode moves to Agent SDK/API
  billing.
- Added retry handling for transient usage endpoint failures and removed
  duplicate `status` error output.
- Added `limitping upgrade` / `limitping update` to update the installed binary
  from the latest GitHub release.
- Added `limitping uninstall`, which removes the binary and config/cache by
  default, with `--keep-config` to preserve config/cache.
- Added open-source governance, security, privacy, and contribution guidance.

## v0.1.0

- Initial public release target.
