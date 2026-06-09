# Package: `ui`

## Purpose

Implements the interactive Bubble Tea terminal view, rendering split fader layout slots, status bars, and the stream bind panel.

## Exported API

```go
package ui

type Dispatcher interface {
	Snapshot() [8]dispatcher.Channel
	Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string, pid uint32)
	Unbind(ch int)
	ToggleMute(ch int)
	ToggleSolo(ch int)
}

type StripConfig struct {
	IsSplit    bool
	KnobLabel  string // DeviceConfig.Label for the knob device
	KnobType   string // "input", "playback", "output", or ""
	FaderLabel string // DeviceConfig.Label for the fader device
	FaderType  string // same
}

type Model struct {
	ActivePage      string
	ChannelAdvanced [8]bool
	// contains filtered or unexported fields
}

func New(disp Dispatcher, snap [8]dispatcher.Channel, labels [8]string, initial []streams.EnrichedStream, stripCfgs [8]StripConfig, reloadFn func() [8]StripConfig, versionMismatch bool) Model

func (m Model) Init() tea.Cmd

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)

func (m Model) View() string

type PageChangedMsg struct {
	Page string
}
```

## Inbound Dependencies

- `cmd/smc-mixer`

## Outbound Dependencies

- `audio`
- `dispatcher`
- `midi`
- `streams`

## Seams

- **`Dispatcher` Interface**: Decouples UI client updates from specific IPC socket handlers.
- **`New` Constructor**: Accepts configuration reload hooks and initialization snapshots.

## Side Effects

- Mutates terminal buffers (toggles alternate screens, handles user keyboard reads via Bubbletea).

## Package-level Invariants & Concurrency Assumptions

- Relies on Bubble Tea's single-threaded event loop design.
