# Repository Guidelines

## Project Structure & Module Organization

This is a Go module for a MIDI-controlled PipeWire mixer with a daemon and TUI client.

- `cmd/smc-mixerd/`: daemon entrypoint.
- `cmd/smc-mixer/`: terminal UI client entrypoint.
- `daemon/`: local client/server protocol and daemon wiring.
- `dispatcher/`: routes MIDI/control events to mixer actions.
- `midi/`: MIDI parsing, classification, device access, and listener logic.
- `pipewire/`: PipeWire client, parsing, and crossfader behavior.
- `streams/`: stream enrichment from Hyprland and MPRIS.
- `ui/`: Bubble Tea model, view, styles, keys, and UI tests.
- `config/`, `audio/`: configuration loading and shared audio domain types.
- `src/`: TOML mapping examples; `config-example.yaml` documents runtime config shape.

## Build, Test, and Development Commands

- `make build`: builds `smc-mixerd` and `smc-mixer` in the repository root.
- `make test`: runs `go test ./...` across all packages.
- `make install`: builds both binaries and installs them into `~/.local/bin`.
- `make clean`: removes generated root binaries.
- `go test ./ui -run TestName`: runs a focused package test while iterating.

## Coding Style & Naming Conventions

Use standard Go style: tabs from `gofmt`, package names in lowercase, exported names in `PascalCase`, unexported names in `camelCase`, and tests named `TestXxx`. Keep packages organized around behavior, as the current layout does. Prefer small domain types over loose strings when modeling audio nodes, streams, MIDI events, or daemon messages.

Run `gofmt` on touched Go files before committing. Avoid broad rewrites unless the change requires them.

## Testing Guidelines

Tests use Go's built-in `testing` package and live beside source files as `*_test.go`. Add or update focused tests for parser behavior, dispatcher routing, config loading, stream enrichment, and UI state transitions. Use `make test` before opening a PR.

## Commit & Pull Request Guidelines

Recent history uses short conventional-style prefixes such as `feat:`, `refactor:`, and `chore:`. Keep commit subjects imperative and specific, for example `feat: add pickup mode for sink channels`.

PRs should include a concise summary, testing performed, and any runtime/config impact. Link related issues when available. Include screenshots or terminal recordings for visible TUI changes.

## Agent-Specific Instructions

When answering questions about libraries, frameworks, SDKs, APIs, CLI tools, or cloud services, use `ctx7` first to fetch current documentation. Resolve with `npx ctx7@latest library <name> "<question>"`, then fetch docs with `npx ctx7@latest docs <libraryId> "<question>"`.
