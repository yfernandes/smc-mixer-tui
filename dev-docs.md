# smc-mixer Developer Documentation

## Architecture Overview

`smc-mixer` is a Linux-based audio mixer ecosystem designed for the M-Vave SMC-Mixer (and compatible MCU) MIDI control surface. It interfaces with the PipeWire audio server to provide real-time hardware control over application volumes, microphones, and output sinks.

The project is split into two primary components:
1.  **`smc-mixerd` (Daemon):** A background service that manages MIDI I/O, discovers audio streams, polls volume levels, and handles complex routing like crossfading.
2.  **`smc-mixer` (Client/TUI):** A terminal-based user interface built with the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework that connects to the daemon for visualization and manual control.

```
+-------------------------------------------------------------+
|                     smc-mixerd (Daemon)                     |
|                                                             |
|   +------------------+                   +--------------+   |
|   |  midi.Listener   |--[ midi.Msg ]---->|              |   |
|   +------------------+                   |              |   |
|                                          |              |   |
|   +------------------+                   | dispatcher.  |   |
|   | pipewire.Client  |<--[ SetVol/Mute ]-|  Dispatcher  |   |
|   +------------------+                   |              |   |
|                                          |              |   |
|   +------------------+                   |              |   |
|   | streams.Enricher |                   +--------------+   |
|   +------------------+                           |          |
|            |                                     v          |
|            +---------[ EnrichedStreams ]---+     |          |
|                                            v     v          |
|                                      +--------------+       |
|                                      | daemon.Server|       |
|                                      +--------------+       |
+----------------------------------------------|--------------+
                                               | (Unix Socket SocketPath)
                                               v (newline-delimited JSON)
+-------------------------------------------------------------+
|                      smc-mixer (TUI)                        |
|                                                             |
|              +--------------------------------+             |
|              |         daemon.Client          |             |
|              +--------------------------------+             |
|                               | (bubbletea updates)         |
|                               v                             |
|              +--------------------------------+             |
|              |            ui.Model            |             |
|              +--------------------------------+             |
+-------------------------------------------------------------+
```

## Package Structure

### Core Logic
- **`cmd/smc-mixerd/`**: The daemon entry point. Orchestrates the stream discovery loop, MIDI listener, and IPC server.
- **`dispatcher/`**: The central state machine. It manages the 8-channel mixer strips, mapping MIDI events (faders, knobs, buttons) to PipeWire actions. It handles volume debouncing, fader pickup logic (sync), and advanced mode toggling.
- **`config/`**: Handles YAML configuration parsing, device matching logic (regex, title, substring), and per-page slot assignments.
- **`daemon/`**: Implements the Unix domain socket IPC protocol. Uses newline-delimited JSON envelopes for bidirectional communication.

### Audio & Hardware Abstraction
- **`pipewire/`**: A wrapper around `wpctl`, `pactl`, `pw-dump`, and `pw-metadata`. It parses JSON output from `pw-dump` and executes shell commands to adjust volume, mute, and routing.
- **`midi/`**: Handles raw MIDI byte stream parsing with running-status support. Converts raw bytes into typed messages (`FaderMsg`, `KnobMsg`, etc.) and provides a writer for LED feedback.
- **`streams/`**: Enriches basic PipeWire stream info by joining it with Hyprland window metadata (via `hyprctl`) and MPRIS media player state (via D-Bus).
- **`audio/`**: Defines basic constants for node types (`KindSource`, `KindMic`, `KindSink`).

### User Interface
- **`ui/`**: The Bubble Tea TUI. Renders the 8-channel strips, the bind picker, and the status bar. It receives state updates from the daemon via the IPC client.

## Data Flow & Control Loops

### 1. MIDI Input Loop (`midi/listener.go` & `dispatcher/run.go`)
- Raw MIDI bytes are read from `/dev/snd/midiC*D0`.
- The `midi` package classifies these into domain messages.
- The `dispatcher` updates internal `Channel` state and triggers `pipewire` actions.

### 2. Stream Discovery & Enrichment (`cmd/smc-mixerd/main.go`)
- The `streams.Enricher` polls PipeWire, Hyprland, and MPRIS every 2 seconds.
- The daemon compares the live streams against the `config` to plan and apply bindings.
- Updates are broadcast to all connected TUI clients.

### 3. Volume Polling (`cmd/smc-mixerd/main.go`)
- A high-frequency (50ms) ticker polls `wpctl get-volume` for all bound streams.
- This ensures the TUI and channel strip LEDs stay in sync with external volume changes (e.g., in-app volume sliders). Since the physical faders are not motorized, the daemon cannot move them, making the fader pickup (sync) procedure essential.

### 4. IPC Protocol (`daemon/proto.go`)
- **Push (Daemon -> Client):** Full state on connect (`initial`), channel snapshots, live stream lists, and MIDI status.
- **Command (Client -> Daemon):** Manual `bind`, `unbind`, `mute`, and `solo` requests.

## Key Design Decisions

### Fader Pickup Logic (Sync)
The M-Vave SMC-Mixer does not feature motorized faders. Consequently, the physical faders cannot be moved by the daemon to match volume changes occurring in software (such as manual in-app volume adjustments or during initial stream bindings). 

To prevent sudden, jarring volume jumps when a fader is touched, `smc-mixer` implements a "pickup" mechanism:
- Newly bound streams start as "unsynced".
- The physical fader must be moved until it passes through the stream's actual volume level (or is brought all the way down to zero) before it takes control.
- The TUI displays a blinking arrow (↑/↓) next to the volume percentage indicating which direction the physical fader must be moved to achieve sync.
- Once synchronized, the fader takes over PipeWire volume adjustments.

### Advanced Mode
Channels can be toggled into "Advanced Mode" (via a short press of the 'R' button on a bound strip containing an `advanced` block). In this mode:
- The R LED blinks.
- Fader and Knob inputs are remapped to custom effects or actions defined in the config's `advanced` block (e.g. reverb, echo).
- The `advanced` block is declared in config (see `config-example.yaml`), but the dispatcher's handling of the declared effects and actions is not yet fully implemented.

### Crossfader Routing
Crossfading is implemented using PulseAudio's `module-null-sink` and `module-loopback`. The daemon builds a graph:
`Stream -> NullSink -> Loopback A -> GainSink A -> Loopback 2A -> Sink A`
`                  -> Loopback B -> GainSink B -> Loopback 2B -> Sink B`
The knob controls the volumes of `GainSink A` and `GainSink B` to achieve a plateau crossfade.

## How to Build & Test
- **Build:** `make build` (creates `smc-mixer` and `smc-mixerd`).
- **Install:** `make install` (places binaries in `~/.local/bin`).
- **Test:** `go test ./...`.
- **Integration Test:** `make test-pipewire-integration` for crossfader logic.
