# Changelog

All notable changes to this project should be documented here.

This project uses version tags such as `v0.2.0`. Release binaries are published
through GitHub Actions and GoReleaser.

## Unreleased

- Nothing yet.

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
