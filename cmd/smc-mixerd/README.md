# Package: `main` (Daemon: `cmd/smc-mixerd`)

## Purpose
Serves as the background system daemon binary entrypoint. Manages background polling routines, handles automatic device routing, and matches streams to active physical configurations.

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
- `dispatcher`
- `midi`
- `pipewire`
- `streams`

## Seams
- **Compiled Daemon Binary**: The `smc-mixerd` executable.

## Side Effects
- Spawns background worker loops (`routeMIDI`, `runMIDIDeviceLoop`, `runVolumePoller`, `pollStreams`).
- Reads and updates fader pins stored in `$XDG_DATA_HOME/smc-mixer/pinned.yaml`.
- Performs writes to hardware LEDs and registers virtual modules in PipeWire.
- Note: Although it attempts to write motorized fader values via `midi.Writer.SetFaderPosition`, the physical faders on the target M-Vave SMC-Mixer hardware are not motorized and will not physically move.

## Package-level Invariants & Concurrency Assumptions
- Employs channel pipelines and mutex boundaries to isolate configuration updates from MIDI events.
