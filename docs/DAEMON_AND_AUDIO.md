# Daemon lifecycle & audio routing — the survival guide

This document exists because debugging "the fader/crossfader stopped working" without
it is a hellhole. It captures the process model, the PipeWire routing, the failure
modes we've actually hit, and how to diagnose each one. Read the [architecture](#architecture)
and the [troubleshooting table](#troubleshooting-symptom--cause--fix) first.

---

## Architecture

Two binaries, strict roles:

| Binary        | Role | Lifecycle |
|---------------|------|-----------|
| `smc-mixerd`  | **The driver.** Owns the MIDI device, PipeWire control, stream discovery, the crossfader modules, and the IPC socket. | Long-running **singleton**, managed by `systemctl --user`. Runs with or without a client. |
| `smc-mixer`   | **A pure client (TUI).** Connects to the daemon socket, renders state, sends bind/unbind. | Ephemeral. Attaches to the running daemon; **never spawns one.** |

Rules that keep this sane — do not violate them:

1. **Exactly one daemon.** Enforced by a `flock` singleton lock (`cmd/smc-mixerd/singleton.go`,
   acquired first thing in `main`). A second `smc-mixerd` logs `already running; exiting`
   and exits 0.
2. **The TUI is a client only** (`cmd/smc-mixer/daemon_start.go`). If no daemon is running it
   errors with `systemctl --user start smc-mixer.service` — it does **not** start its own.
3. **The daemon is the sole owner** of the MIDI device and the `smc_*` PipeWire modules.

### Running it

```bash
systemctl --user enable --now smc-mixer.service   # start now + on boot
systemctl --user status smc-mixer.service
journalctl --user -u smc-mixer.service -f         # live logs
```

Health check (should always print `1`):

```bash
pgrep -c smc-mixerd
```

> **If you ever see 2+ daemons, that is the bug.** It predates the singleton lock and was
> the root cause of most "unresponsive device" / "dead crossfader" incidents. See
> [Daemon stacking](#1-daemon-stacking).

---

## The crossfader signal chain

When a channel's knob is `type: send` (a crossfade between two output buses), the daemon
builds this PipeWire graph per app (`pipewire/crossfader.go`, `cmd/smc-mixerd/crossfaders.go`).
`<tag>` is the device key from `config.yaml` (e.g. `firefox`):

```
app stream ─▶ smc_<tag>_void         (null sink; FADER sets volume here / on the stream)
                 │ monitor
                 ├─▶ loopback ─▶ smc_<tag>_gain_a   [KNOB sets this vol = bus A gain]
                 │                     │ monitor
                 │                     └─▶ loopback ─▶ Sink A  (e.g. headphones)
                 └─▶ loopback ─▶ smc_<tag>_gain_b   [KNOB sets this vol = bus B gain]
                                       │ monitor
                                       └─▶ loopback ─▶ Sink B  (e.g. speakers)
```

- **Fader** → volume on the bound app stream node (per-stream; see [multi-tab](#3-multi-tab-per-stream-binding)).
- **Crossfade knob** → `smc_<tag>_gain_a` / `_gain_b` sink volumes (via `pactl set-sink-volume`,
  by name, so it survives node-ID churn). `gain_a=1,gain_b=0` = all to bus A.
- The 4 loopback modules are created with `sink.dont.move=true source.dont.move=true` so a
  WirePlumber relink can't scramble them (see [WP restart](#5-wireplumber-restart-scrambles-the-chain)).

The knob only reaches the gains if the channel is on a page where the app has a `send` knob.
On the `main` page firefox has a fader but `knobs: 2` is unset — **no crossfade knob there.**
The crossfade knob lives on the `applications` page (per `defaults.playback-knob`).

---

## WirePlumber integration & the create→move race

A stream obeys the fader/crossfader **only while it flows through `smc_<tag>_void`.** New app
streams are born on the **default sink** and play there audibly, *bypassing* the chain, until
the daemon's ~2 s poll moves them in. During that window the controls appear dead. This is the
single most confusing failure and it masquerades as "routing is broken" (it isn't — trace it
and the graph is intact; the audible stream is just outside it).

**The fix** is `extras/wireplumber/51-smc-mixer.conf` — a WirePlumber `stream.rules` drop-in
that sets `target.object = "smc_<tag>_void"` on each crossfade app's streams, so WirePlumber
routes them into the void **at creation**, before the first sample. Install it (see the file's
header) and it is required for the fader/crossfader to work reliably with apps that spawn
streams on the fly (browsers especially).

Key facts learned the hard way:

- The correct key is **`stream.rules`**, not `node.rules`. WP 0.5 applies it via
  `scripts/node/state-stream.lua` (the **restore-stream** hook).
- WirePlumber's own **restore-stream** is the "outside influence" — it remembers/restores a
  stream's last target. It is not caelestia or the GPU. (caelestia only sets
  `preferredDefaultAudioSink`; it never moves individual streams.)
- Match on `application.process.binary` (stable across tabs), not the enriched name. The daemon
  matches apps by MPRIS-enriched identity (`firefox.*`); WirePlumber only sees the raw
  `Zen`/`zen-bin`. This impedance mismatch is why the rule can't be derived from `config.yaml`
  alone yet — see [Open issues](#open-issues--planned-work).

---

## Troubleshooting: symptom → cause → fix

| Symptom | Likely cause | Fix / check |
|---|---|---|
| Controller totally unresponsive; erratic | **[Daemon stacking](#1-daemon-stacking)** — 2+ `smc-mixerd` fighting over MIDI | `pgrep -c smc-mixerd` must be `1`. `systemctl --user restart smc-mixer.service`. Singleton lock now prevents new stacks. |
| Fader moves but app volume doesn't change; audio plays fine | Bound stream is **not in the void** (create→move race), or it's a **different tab** than the audible one | Install the [WP drop-in](#wireplumber-integration--the-createmove-race). Confirm: `pw-link -l \| grep 'Zen:output'` should point at `smc_<tag>_void`, not the hardware sink. |
| Crossfade knob does nothing | Wrong page (no `send` knob), **or** crossfader [churn](#4-crossfader-churn), **or** stacked daemons | Use the `applications` page. Check logs for `crossfader … tearing down`. `pgrep -c smc-mixerd == 1`. |
| Crossfader worked, then stopped after a stream/tab change | **[Crossfader churn](#4-crossfader-churn)** — routing keyed to an ephemeral stream ID | Currently self-heals on rebind; see [open issues](#open-issues--planned-work). |
| Audio routes to wrong device after a WirePlumber update/restart | **[WP restart scrambled loopbacks](#5-wireplumber-restart-scrambles-the-chain)** | Restart the daemon to rebuild (`systemctl --user restart smc-mixer.service`). Loopbacks are now pinned; prefer relogin over WP restart. |
| Only one browser tab responds to the fader | **[Multi-tab binding](#3-multi-tab-per-stream-binding)** — fader controls one stream node | Bind the specific stream in the TUI, or keep one audio tab. Per-app grouping is planned. |

### 1. Daemon stacking

The daemon owns the MIDI device (exclusive) and the `smc_*` modules. A second instance grabs
MIDI, steals the socket (`Server.Listen` used to `os.Remove` it unconditionally), and runs
`CleanupCrossfaderTag`, tearing down the first's crossfader graph. Result: dead controller,
dead knob. Historically caused by the TUI auto-spawning a detached daemon that collided with
the systemd one. **Prevented now** by the singleton `flock` + the TUI being client-only, but
if you launch `smc-mixerd` by hand while the service runs, the hand-launched one exits.

```bash
pgrep -a smc-mixerd                      # expect exactly one, PPID = systemd
for p in $(pgrep smc-mixerd); do echo "$p: $(ls -l /proc/$p/fd | grep -c snd) MIDI fds"; done
```

### 3. Multi-tab per-stream binding

Each browser audio tab is a separate PipeWire stream, but the enricher gives them the **same**
identity (browsers expose one global MPRIS player + one PID). The fader binds to **one** stream
node, so only that tab responds. See `[[project_multitab_binding]]` in agent memory. Intended
behavior for now (one fader ↔ one stream); an "application group" feature is planned.

### 4. Crossfader churn

`crossfaderManager.Sync` keys each routing to a specific `streamID`. When that stream vanishes
(tab closed, new video → new stream ID), Pass 1 logs `stream N not in ss, tearing down` and
rebuilds the whole crossfader around the new stream — recreating modules and flapping the knob
attachment. It self-heals but is disruptive. The routing should instead be anchored to the
**device + void sink**, independent of any single stream. See [open issues](#open-issues--planned-work).

### 5. WirePlumber restart scrambles the chain

Restarting WirePlumber re-links every node. The crossfader loopbacks are now created with
`sink.dont.move=true source.dont.move=true`, so they survive. If you still see mis-routing after
a WP restart/update, restart the daemon to rebuild from scratch. In normal boot order WP starts
before the daemon, so this only bites on mid-session WP restarts — prefer a fresh login.

---

## Diagnostic cheat-sheet

```bash
# One daemon, systemd-managed?
pgrep -c smc-mixerd ; systemctl --user status smc-mixer.service

# Crossfader modules present? (expect 7: 3 null-sinks + 4 loopbacks per crossfade app)
pactl list modules short | grep smc_

# Where is an app's audio actually going? (want smc_<tag>_void, not the hardware sink)
pw-link -l | grep -A2 'Zen:output'

# Is the WP drop-in applying target.object at creation?
pw-dump | python3 -c 'import json,sys;[print(o["id"],(o.get("info") or {}).get("props",{}).get("target.object")) for o in json.load(sys.stdin) if (o.get("info") or {}).get("props",{}).get("application.process.binary")=="zen-bin"]'

# Crossfader gain volumes (what the knob drives)
wpctl status | grep -iE 'smc_.*gain'

# Daemon's view of channels (bindings, crossfader attach, sync)
journalctl --user -u smc-mixer.service | grep -i crossfader | tail
```

---

## Open issues / planned work

- **Crossfader decoupling.** Anchor each routing to the device + `smc_<tag>_void` instead of an
  ephemeral `streamID`, so it stops [churning](#4-crossfader-churn) on tab/stream changes.
- **Generate the WP drop-in from config.** Today `extras/wireplumber/51-smc-mixer.conf` is
  installed by hand. A generator needs the raw `application.process.binary` per app (learn it
  from the live bound stream, or add a `pw-match:` config override), then write the drop-in and
  prompt for a one-time `systemctl --user restart wireplumber`.
- **Application grouping.** Let all of an app's streams share one fader/group (fixes
  [multi-tab](#3-multi-tab-per-stream-binding) and lets a browser be controlled as a unit).
