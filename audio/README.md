# Package: `audio`

## Purpose

Provides basic enumerations and shared domain types representing PipeWire node types.

## Exported API

```go
package audio

type NodeKind uint8

const (
	KindSource NodeKind = iota // app playing audio
	KindMic                    // microphone / capture device
	KindSink                   // output device / speakers
)
```

## Inbound Dependencies

- `config`
- `daemon`
- `dispatcher`
- `pipewire`
- `streams`
- `ui`
- `cmd/smc-mixerd`

## Outbound Dependencies

None

## Seams

- **`NodeKind`**: A simple classification type representing `KindSource`, `KindMic`, or `KindSink` passed across packages to filter matching logic.

## Side Effects

None

## Package-level Invariants & Concurrency Assumptions

Constants are immutable and read-only.
