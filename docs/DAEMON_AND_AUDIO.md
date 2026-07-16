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
builds this PipeWire graph per app (`pipewire/crossfader.go`, `backend/pwbackend/crossfader.go`).
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
  stream's last target. The daemon therefore pins live `target.object` metadata before its
  explicit move as well as using the creation-time drop-in. It is not caelestia or the GPU. (caelestia only sets
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
| Crossfade knob does nothing | Wrong page (no router `knob: crossfade` mapping), missing WirePlumber rule, or stacked daemons | Use the promoted/router `applications` page, confirm the stream targets `smc_<tag>_void`, and check `pgrep -c smc-mixerd == 1`. |
| Crossfader worked, then stopped after a stream/tab change | Replacement stream did not attach to the stable void sink | Check for `attach replacement stream` errors. The backend reattaches resolved replacements without rebuilding the graph; the WirePlumber drop-in still prevents the initial creation race. |
| Audio routes to wrong device after a WirePlumber update/restart | **[WP restart destroyed or scrambled the graph](#5-wireplumber-restart-scrambles-the-chain)** | Wait for the daemon's next reconciliation; it detects missing graph sinks and rebuilds automatically. If recovery still fails, restart `smc-mixer.service`. |
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

### 4. Crossfader graph ownership

`backend/pwbackend` keys each routing to the configured device/rule and its stable
`smc_<tag>_void` sink. A temporary resolution gap keeps the graph alive; when a replacement
stream resolves, the backend moves it into that existing void sink without tearing down and
recreating the seven modules. WirePlumber still attaches newly created streams at creation time
to eliminate the polling window before backend reconciliation.

### 5. WirePlumber restart scrambles the chain

Restarting WirePlumber re-links every node and can destroy the module-backed void/gain sinks
while the daemon remains alive. The backend checks all three device-owned sinks during each
reconciliation; if they disappeared, it discards the stale module IDs and rebuilds the graph.
If automatic recovery still fails, restart the daemon. In normal boot order WP starts before
the daemon, so this mostly matters for mid-session WP restarts — prefer a fresh login.

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

# Daemon's view of backend-owned crossfader lifecycle
journalctl --user -u smc-mixer.service | grep -i crossfader | tail
```

---

## Debug instrumentation

Built-in, env-gated hooks — all inert by default, safe to leave enabled in a
dev session. They exist because "the TUI blinks" class of bugs cannot be
diagnosed from either side alone: you need to know *what arrived* (daemon
side) and *what actually re-rendered* (client side) at the same time.

| Hook | Where | What it does |
|---|---|---|
| `SMC_TUI_DEBUG=<file>` | `smc-mixer` (client) | Logs every non-tick `Update` msg type and a line-level diff of each frame whose rendered content changed (`ui/debug_instrument.go`). An idle TUI should log ~zero `FRAME CHANGED` lines. |
| `SMC_ROUTER_DEBUG=1` | `smc-mixerd` | Logs every `router.notify()` with its caller — attributes strip-broadcast (`kindStrips`) churn to the code path that caused it. |
| `SMC_POLLER_DEBUG=1` | `smc-mixerd` | Logs volume-poller tick/broadcast counters every 2 s — attributes snapshot-broadcast (`kindSnapshot`) churn. |
| `kill -USR1 <daemon pid>` | `smc-mixerd` | Injects a synthetic Play button press (toggles the applications page) so a headless session can exercise router pages without the physical controller. |

### Headless repro playbook

How the 2026-07 applications-page blinking was isolated; reusable for any
"UI misbehaves, cause unknown" bug without touching the hardware:

```bash
# 1. Daemon with tracing, logs to a stable path
SMC_ROUTER_DEBUG=1 SMC_POLLER_DEBUG=1 nohup ~/.local/bin/smc-mixerd \
  > ~/.cache/smc-mixer/smc-mixerd.log 2>&1 & disown

# 2. Flip to the applications page without the controller
kill -USR1 "$(pgrep smc-mixerd)"

# 3. Run the TUI in a pty for 30 s, capturing frame diffs
SMC_TUI_DEBUG=/tmp/tui-debug.log timeout 30 script -qec smc-mixer /dev/null
grep -c "FRAME CHANGED" /tmp/tui-debug.log   # idle: expect 1 (initial paint)

# 4. Inspect what the daemon actually sends (initial state, one JSON frame)
python3 -c "
import socket,json
s=socket.socket(socket.AF_UNIX); s.connect('/run/user/1000/smc-mixer.sock')
buf=b''
while b'\n' not in buf: buf+=s.recv(65536)
env=json.loads(buf.split(b'\n')[0])
for st in env['data'].get('strips',[]):
    print(st['strip'], st.get('target_id'), json.dumps(st.get('params')))
"
```

Reading the results: `FRAME CHANGED` lines with **no** corresponding client
`update:` lines (other than the 50 ms tick) mean the render itself is
nondeterministic — inspect the logged line diffs for which strip/param
alternates. `FRAME CHANGED` paired with `StripsMsg`/snapshot arrivals means
daemon-side churn — use `SMC_ROUTER_DEBUG`/`SMC_POLLER_DEBUG` to attribute it.

### Case study: the applications-page blink (2026-07, issue router-refactor/05)

Symptom: the TUI flickered at the tick rate on the applications page while
completely idle; every server-broadcast theory failed (broadcasts measured
near-zero). The playbook above found two compounding bugs in under an hour
after days of daemon-side hunting:

1. `router.snapshotLocked` published assignment params the backend never
   declared; `specFor`'s fallback stamps them `backend.ParamContinuous`,
   which is **iota zero** — so a phantom `solo {kind:0}` rode along on every
   strip, masquerading as a second fader. (The socket dump in step 4 made
   this visible immediately.)
2. `ui/generic_strip.go` picked "the fader" by ranging over the `s.Params`
   map and taking the first Continuous param. Go randomizes map iteration
   order **per call**, so with two kind-0 params the strip rendered a
   different param nearly every 50 ms tick. Only the crossfade strip
   visibly blinked because only there did `volume` differ from the
   phantom's zero value.

Morals: a zero-valued enum (`ParamContinuous = iota`) is a dangerous
default — spec-less params must be filtered, not defaulted; and any
"first match wins" scan over a Go map in render code is a frame-to-frame
coin flip. Regression tests: `TestSnapshotOmitsParamsWithoutBackendSpec`
(router), `TestFaderParamDeterministicWithMultipleContinuousParams` and
`TestRenderGenericStripDeterministic` (ui).

---

## Open issues / planned work

- **Generate the WP drop-in from config.** Today `extras/wireplumber/51-smc-mixer.conf` is
  installed by hand. A generator needs the raw `application.process.binary` per app (learn it
  from the live bound stream, or add a `pw-match:` config override), then write the drop-in and
  prompt for a one-time `systemctl --user restart wireplumber`.
- **Application grouping.** Let all of an app's streams share one fader/group (fixes
  [multi-tab](#3-multi-tab-per-stream-binding) and lets a browser be controlled as a unit).
