# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-05-19

### Added
- Optional `working_copies_dir` top-level key in `.mws.toml`. When set, `mws clone` and `mws init` place working copies at `<meta>/<working_copies_dir>/<name>/` instead of directly under the meta root; every other working-copy-aware subcommand (`list`, `rm`, `relink`, `add-repo`, `promote`, `sync-env`, `stage-env`) honors the same root. Default empty preserves prior behavior.
- `mws init` now prompts for the working-copies subdirectory (optional) alongside the project name, description, and parent directory.

### Changed
- `mws list`, `mws relink`, and `mws rm` now surface a clear parse error when `.mws.toml` is malformed instead of silently scanning the meta root with default values.

## [0.1.2] - 2026-05-19

### Fixed
- `mws clone` now retargets the new working copy's `origin` to the URL declared in `.mws.toml` after a `git clone --local` from a sibling. Previously the new clone's `origin` pointed at the local sibling directory, so `git push`/`pull`/`fetch` missed the canonical remote.

### Added
- Root `.gitignore` covering goreleaser output, the root-level `mws` binary, test/coverage artifacts, per-user agent dirs, and editor/OS cruft.

## [0.1.1] - 2026-05-17

### Changed
- Homebrew formula now lives at the canonical `Formula/mws.rb` path in the tap. Existing `brew install sustinbebustin/mws/mws` continues to work unchanged.

## [0.1.0] - 2026-05-17

### Added
- MIT license.
- `mws version` subcommand prints the version, commit SHA, and build date (populated at release time).
- GitHub Actions CI runs `go build`, `go test`, `golangci-lint`, and `govulncheck` on Linux and macOS for every pull request and push to `main`.
- Tag-driven release pipeline: pushing a `v*` tag publishes darwin and linux binaries (amd64 and arm64) to GitHub Releases via goreleaser, with checksums and a conventional-commit-grouped changelog.
- Homebrew tap: every release pushes a formula to `sustinbebustin/homebrew-mws`, enabling `brew install sustinbebustin/mws/mws`.

[Unreleased]: https://github.com/sustinbebustin/mws/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/sustinbebustin/mws/releases/tag/v0.2.0
[0.1.2]: https://github.com/sustinbebustin/mws/releases/tag/v0.1.2
[0.1.1]: https://github.com/sustinbebustin/mws/releases/tag/v0.1.1
[0.1.0]: https://github.com/sustinbebustin/mws/releases/tag/v0.1.0
