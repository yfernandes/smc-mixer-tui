# smc-mixer User Documentation

## Introduction

`smc-mixer` is a Linux application that gives you physical control over your PipeWire audio streams using a MIDI controller (specifically optimized for the M-Vave SMC-Mixer or MCU-compatible devices). It automatically maps your favorite apps, microphones, and speakers to hardware faders and knobs.

## Installation & Setup

### Prerequisites
- Linux with PipeWire and PipeWire-Pulse installed.
- `wpctl`, `pactl`, `pw-dump`, and `pw-metadata` command-line tools.
- A compatible MIDI device.

### Installation
1.  Clone the repository and run `make install`.
2.  The binaries `smc-mixer` and `smc-mixerd` will be installed to `~/.local/bin/`.
3.  Ensure your MIDI device is plugged in.

### Starting the App
Run `smc-mixer`. It will automatically launch the background daemon (`smc-mixerd`) if it isn't already running.

## Configuration

Configuration is stored in `~/.config/smc-mixer/config.yaml`.

### Example Config
```yaml
midi:
  device: "" # auto-discovered

defaults:
  input-knob:
    type: gain
  playback-knob:
    type: send
    bus-a: headphones
    bus-b: speakers
  output-knob:
    type: none

devices:
  spotify:
    label: "Spotify"
    type: playback
    match: spotify
  firefox:
    label: "Firefox"
    type: playback
    match-regex: "firefox.*"
  fifine:
    label: "Fifine Mic"
    type: input
    match: "fifine Microphone Analog Stereo"
    knob:
      type: gain
  speakers:
    label: "Speakers"
    type: output
    match: "Analog Stereo"
  headphones:
    label: "Headphones"
    type: output
    match: "WH-1000XM4"

pages:
  main:
    button: none
    faders:
      0: spotify
      1: firefox
      7: ~         # unassigned
    knobs:
      0: fifine
      6: headphones
      7: speakers

  applications:
    button: play
    channels:
      0: spotify
      1: firefox
```

The `main` page uses separate `faders:` and `knobs:` maps so you can split a strip between two devices (e.g. fader controls a playback app, knob controls a mic). Other pages use `channels:` which binds the whole strip (fader + knob) to one device.

### Matchers
- `match`: Substring match on the application or node name.
- `match-regex`: Regular expression match.
- `match-title`: Substring match on the window title (requires Hyprland).

## Using the TUI (Terminal Interface)

The TUI displays 8 channel strips representing your hardware controls.

### Visual Cues
- **Colored Borders**: Red for microphones, Green for apps, Blue for outputs.
- **Blinking Arrow (↑/↓)**: Fader Pickup Mode. Because the physical faders on the M-Vave SMC-Mixer are not motorized, they cannot automatically move when volume changes in software. You must move your physical fader in the direction of the arrow to "pick up" the current volume level. Once aligned, the physical fader is synchronized and takes control.
- **[M], [S], [R], [■]**: Status of Mute, Solo, Record/Advanced, and Playback/Stop buttons.

### Keyboard Shortcuts
- `q` / `Ctrl+C`: Quit.
- `Left` / `Right`: Select channel strip.
- `Enter`: Open the **Bind Menu** to manually assign a stream to the selected strip.
- `u`: Unbind the selected strip.
- `r`: Reload configuration.
- `↑` / `↓` (in Bind Menu): Navigate stream list.

## Hardware Controls

### Faders & Knobs
- **Faders**: Control stream volume. Since the faders are not motorized, their physical positions do not update automatically when software volumes change. You must use the Fader Pickup Mode (sync procedure) to align them.
- **Knobs**:
  - **Gain Mode**: Independent volume control (usually for microphones or secondary devices).
  - **Send/Crossfade Mode**: Routes audio between two buses (e.g., Headphones ↔ Speakers). The center position plays to both; turning left or right fades one out.

### Buttons
- **Mute**: Toggles mute.
- **Solo**: Solos the channel (mutes all others of the same type).
- **Stop (■)**: Toggles Play/Pause via MPRIS (if supported by the app).
- **Rec (R)**: 
  - **Short Press**: Toggles custom "Record" state or enters **Advanced Mode**.
  - **Long Press (0.5s)**: Pins the current stream to the channel on the "main" page.

### Transport Buttons (Pages)
The transport buttons switch between mixer "pages". Each page is defined in your config with a `button:` field that maps it to a hardware button. The default mapping from `config-example.yaml` is:
- **Play**: `applications` page.
- **Record** (`rec`): `inputs` page.
- **Pause**: `outputs` page.
- **Previous** (`prev`): `system` page.
- **Next**: `custom` page.

You can remap these by changing the `button:` value on any page in your config.

## Advanced Mode
If a device has an `advanced` configuration block, pressing the **R** button enables Advanced Mode. The R LED will blink, and your fader/knobs may be remapped to control effects (like echo or reverb) or trigger specific actions. Press **R** again to exit.
