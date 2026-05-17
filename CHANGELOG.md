# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- MIT license.
- `mws version` subcommand prints the version, commit SHA, and build date (populated at release time).
- GitHub Actions CI runs `go build`, `go test`, `golangci-lint`, and `govulncheck` on Linux and macOS for every pull request and push to `main`.
- Tag-driven release pipeline: pushing a `v*` tag publishes darwin and linux binaries (amd64 and arm64) to GitHub Releases via goreleaser, with checksums and a conventional-commit-grouped changelog.
- Homebrew tap: every release pushes a formula to `sustinbebustin/homebrew-mws`, enabling `brew install sustinbebustin/mws/mws`.

[Unreleased]: https://github.com/sustinbebustin/mws/commits/main
