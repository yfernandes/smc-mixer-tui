# Package: `main` (TUI Client: `cmd/smc-mixer`)

## Purpose

Serves as the TUI terminal application entrypoint. Dials the local socket, launches the daemon process if absent, and instantiates the TUI model.

## Exported API

```go
package main

var Version = "dev"
```

## Inbound Dependencies

None (executable entrypoint)

## Outbound Dependencies

- `config`
- `daemon`
- `ui`

## Seams

- **Compiled Client Binary**: The `smc-mixer` TUI client.

## Side Effects

- Launches background detached processes (`smc-mixerd`) if socket connection fails.
- Establishes Unix domain socket connections.
- Catches system termination signals.

## Package-level Invariants & Concurrency Assumptions

- Relies on standard signal handling and Bubble Tea's main loop.
