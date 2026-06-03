# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`smc-mixer` is a Go application that turns a hardware MIDI controller (Studiologic SMC46) into a software mixer for PipeWire. It has two binaries:

- **`smc-mixerd`** — background daemon: owns MIDI I/O, PipeWire control, stream discovery, and an IPC socket.
- **`smc-mixer`** — Bubble Tea TUI client: connects to the daemon socket and renders mixer state.

## Commands

```bash
make build                  # build both binaries to repo root
make test                   # go test ./...
make install                # build + install to ~/.local/bin
go test ./ui -run TestName  # run a single test by name

# Integration test (requires a running PipeWire session)
make test-pipewire-integration
```

Run `gofmt` on all touched Go files before committing.

## Architecture

### Data flow

```
MIDI device → midi.Listener → midiCh
  → routeMIDI (fan-out: GlobalMsg to srv + disp.OnGlobal; others to dispCh)
  → dispatcher.Run → onFader / onKnob / onButton → pipewire.Client
                                                  → CrossfaderController
                                                  → LEDWriter

pipewire.Client (poll 50 ms) → disp.UpdateActualVolume → srv.BroadcastSnapshot
streams.Enricher (poll 2 s) → applyBindings → manageCrossfaders → srv.BroadcastStreams

daemon.Server (Unix socket) ← smc-mixer TUI client
  ← kindBind / kindUnbind commands → disp.Bind / disp.Unbind
  → kindInitial / kindSnapshot / kindStreams / kindDevice / kindGlobal
```

### Key packages

| Package | Role |
|---|---|
| `midi/` | MIDI parsing (`msg.go`, `parser.go`), device discovery, listener, LED writer |
| `dispatcher/` | Maps MIDI events to PipeWire actions; holds `[8]Channel` state; pickup-mode faders |
| `pipewire/` | Wraps `wpctl` / `pactl` CLI calls; crossfader routing via null-sink + loopback modules |
| `streams/` | Enriches PipeWire streams with Hyprland window focus and MPRIS playback info |
| `daemon/` | Unix-socket IPC: JSON-framed newline-delimited protocol between daemon and TUI |
| `ui/` | Bubble Tea model/view for the TUI client |
| `config/` | YAML config load/save; `KnobConfig` (gain vs crossfade), `BindConfig`, `ChannelConfig` |
| `audio/` | Shared `NodeKind` enum (mic, source, sink) |

### Crossfader signal chain

When a channel knob is configured as `crossfade`, the daemon creates a PipeWire graph:

```
Stream → [fader vol] → NullSink → monitor
                               → LoopA → GainSinkA [crossfader vol A] → Loop2A → SinkA
                               → LoopB → GainSinkB [crossfader vol B] → Loop2B → SinkB
```

Modules are named `smc_<tag>_void`, `smc_<tag>_gain_a`, `smc_<tag>_gain_b`. Stale modules from prior runs are cleaned up on startup via `CleanupCrossfaderTag`.

### Config

Config file: `$XDG_CONFIG_HOME/smc-mixer/config.yaml` (default). See `config-example.yaml` for the full schema. Key concepts:

- `outputs`: alias → PipeWire device description
- `channels["0"–"7"]`: per-strip `bind` (input / playback / output, substring or regex match) and optional `knob` override
- `defaults`: fallback knob behavior per bind type; crossfade knob references output aliases

## Coding conventions

- Standard Go style: `gofmt` tabs, `PascalCase` exports, `camelCase` unexported.
- Packages are organized around behavior; prefer small domain types over loose strings.
- Commit subjects use conventional prefixes: `feat:`, `fix:`, `refactor:`, `chore:`.
- Tests live beside source as `*_test.go`; use Go's `testing` package only.
