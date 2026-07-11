# smc-mixer Context

This repo is moving from an SMC46-specific PipeWire mixer to a generic
control-surface router. The current daemon and TUI remain the shipping product
while the router is introduced beside the legacy dispatcher.

## Current System

`smc-mixerd` is the singleton daemon. It owns the MIDI device, PipeWire control,
stream enrichment, crossfader module lifecycle, and the daemon socket.
`smc-mixer` is a TUI client that renders daemon state and sends commands over
newline-delimited JSON IPC.

The legacy path is:

```text
midi.Listener -> routeMIDI -> dispatcher -> pipewire / streams / LED writer
                              -> daemon snapshot -> TUI
```

The legacy dispatcher is intentionally `[8]`-shaped because the Studiologic
SMC46 has eight physical strips. During the router refactor, that shape is not
converted to slices; it is retired when the dispatcher is deleted.

## Router Vocabulary

### Surface

A **Surface** is a hardware control surface abstraction. It has a
**Descriptor** that says how many strips it exposes, which per-strip controls
exist, and which global controls exist. The SMC46 is the first surface.

A **Control** is a hardware input or output such as a fader, knob, mute button,
solo button, transport button, or page button.

A **Role** is the stable semantic name for a control position on a surface, for
example `fader`, `knob`, `mute`, `solo`, `rec`, `stop`, or `play`.

A **surface event** is an input emitted by a surface:

- absolute controls report a normalized value from 0 to 1;
- relative controls report a delta;
- momentary controls report a press.

A **FeedbackWriter** is the output side of a surface. It can set LEDs and, for
motorized surfaces, set physical control positions. Motor fader writes must stay
conservative because the SMC46 pitch-bend channel can move the physical fader.

### Backend

A **Backend** owns one target domain, such as shell commands, PipeWire streams,
DDC brightness, or Home Assistant lights.

A **Target** is one controllable thing exposed by a backend. Target IDs are
namespaced strings such as `exec:brightness` or `pipewire:node/42`.

A **Parameter** is one controllable aspect of a target, such as `value`,
`volume`, `mute`, `solo`, `playpause`, or `crossfade`.

Parameter kinds are:

- `Continuous`: normalized numeric value.
- `Toggle`: boolean state.
- `Trigger`: fire-and-forget action.
- `Composite`: backend-owned structure controlled through a parameter, such as
  the PipeWire crossfader module graph.

A parameter can be **Readable** when backend state can be fetched, and **Push**
when the backend can stream state changes. Fire-and-forget parameters are valid
and skip pickup mode.

`TargetInfo.Ext` is backend-namespaced opaque JSON. The router and generic IPC
move it without interpreting it; compiled-in TUI backend views may decode it.

### Router

The **Router** maps surface events to backend parameters. It is slice-native
from day one and is not built from dispatcher `[8]` data structures.

An **Assignment** binds one target to one or more surface roles:

```text
assignment target = exec:brightness
assignment params = fader -> value, mute -> power
```

A **Page** is an ordered list of assignments. A page can contain more
assignments than the surface has strips, so the router windows the page onto
the visible physical strips using an offset.

**Pickup** is the soft-sync behavior for absolute controls. When a parameter is
readable, the router seeds the remote value before enabling control, then waits
for the physical control to cross that remote value. Fire-and-forget parameters
do not use pickup.

**Feedback** means LEDs and optional motor positions derived from router state.
The router only writes feedback for router-owned strips.

## Coexistence Rules

Until the deletion phase, the router runs beside the dispatcher:

- `routeMIDI` in `cmd/smc-mixerd/main.go` is the tee point.
- A strip belongs to exactly one world: dispatcher audio or router assignment.
- Generic router IPC runs beside the legacy snapshot IPC until the dispatcher is
  deleted.
- Existing configs must keep loading unchanged until the final config break.
- The advanced config stubs are not the router seam. They model the wrong thing
  and are removed during the PipeWire backend phase.

## PipeWire Vocabulary

A **rule target** is a PipeWire target backed by config matching rules rather
than one current node ID. Rule targets resolve to live streams and re-resolve
when streams die or reconnect.

A **concrete stream target** is a live PipeWire node target, usually selected by
the TUI.

A **solo group** is the set of assignments that should be mutually exclusive
for solo behavior, such as playback streams of the same kind.

The **crossfader** is a backend-owned composite parameter. It owns a PipeWire
module graph and exposes a continuous `crossfade` parameter. The router must not
learn about null sinks, loopbacks, or sink names.

## References

- `tasks/issues/router-refactor/00-overview.md`
- `docs/DAEMON_AND_AUDIO.md`
- `docs/adr/0001-router-beside-dispatcher.md`
