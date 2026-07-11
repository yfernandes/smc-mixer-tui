# ADR 0001: Build the Router Beside the Dispatcher

## Status

Accepted.

## Context

`smc-mixer` began as an SMC46-controlled PipeWire mixer. The current
implementation encodes two assumptions deeply:

- the hardware has exactly eight strips;
- faders, knobs, and buttons control audio concepts directly.

Those assumptions appear across the dispatcher, daemon protocol, TUI, and config
model. The target architecture is a generic control-surface router where the
same hardware can control heterogeneous targets: PipeWire streams, shell
commands, monitor brightness, lights, and future backends.

The repo already has working audio behavior, including soft pickup, LEDs,
stream rebinding, solo, and a fragile but important crossfader module graph.
The refactor must keep the daemon usable at every step.

## Decision

Build a new `surface` -> `router` -> `backend` path beside the existing
dispatcher.

The new path starts with an exec backend as a vertical slice, then gains state,
feedback, pages, a PipeWire backend, and finally crossfader ownership. The
legacy dispatcher remains responsible for existing audio behavior until each
piece has parity in the router path.

The dispatcher's `[8]` model is not converted to slices. It dies by deletion in
the final phase after every live control path has moved to the router.

The router must not be built on `config.AdvancedConfig`,
`config.ControlConfig`, or `dispatcher/advanced.go`. Those stubs are log-only
and model a mode per audio device, while the router needs assignments per strip
and page.

## Consequences

The daemon temporarily has two control worlds. `routeMIDI` is the coexistence
point: it tees events to the router and keeps dispatcher behavior unchanged for
dispatcher-owned strips and pages.

The daemon protocol temporarily has two state families. The legacy audio
snapshot remains for dispatcher-owned strips, while generic strip messages carry
router state. The TUI merges them by visible strip index until the legacy path
is deleted.

Config compatibility is preserved through the additive phases. The single
intentional config break happens at the final deletion phase and is documented
in `MIGRATION.md`.

This approach creates some short-term duplication: pickup, feedback, solo, and
state broadcast behavior are ported into the router instead of imported from
the dispatcher. That duplication is intentional because the dispatcher is
scheduled for deletion.

## Guardrails

- A strip belongs to either the dispatcher world or the router world, never both.
- Existing audio behavior must remain unchanged while the dispatcher owns it.
- Crossfader module names, cleanup, and WirePlumber interaction must preserve
  existing behavior when ownership moves.
- Generic router packages do not import audio-specific packages.
- Backend-specific details travel through target parameters, backend methods,
  and opaque extension payloads.

## References

- `tasks/issues/router-refactor/00-overview.md`
- `tasks/issues/router-refactor/01-kernel-exec-backend.md`
- `docs/DAEMON_AND_AUDIO.md`
