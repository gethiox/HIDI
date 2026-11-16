# HIDI 2.0 architecture

This document describes the structure HIDI 2.0 is being built towards. It is
the reference for where new code belongs. Sections marked *planned* describe
packages that do not exist yet; update this file in the same pull request that
changes the design.

## Goals

- Every input device (keyboard, game controller) becomes one independent MIDI
  instrument that appears and disappears with the device.
- Linux and Windows are first-class. macOS is out of scope for 2.0, but nothing
  in the core may assume a platform.
- No cgo in the core or the headless build, so every target cross-compiles
  with a plain `go build`.
- The engine is usable headless. The terminal views and the graphical UI are
  clients of the same running engine.
- Library code never exits the process or panics on bad input; errors are
  returned and surfaced to the user.

## Overview

```
 input backends            core engine                 output backends
┌───────────────┐   ┌──────────────────────────┐   ┌────────────────────────┐
│ evdev (Linux) │   │ bindings, layers, state  │   │ ALSA sequencer (Linux) │
│ Raw Input     │──▶│ tracking, velocity       │──▶│ Windows MIDI Services  │
│ (Windows)     │   │                          │   │ existing hardware port │
└───────────────┘   └──────────────────────────┘   └────────────────────────┘
        ▲                        ▲                             ▲
        └─────────── instrument: device lifecycle ─────────────┘
                                 ▲
                      IPC ───────┴─────── tray
                       ▲
          CLI views ───┴─── graphical UI
```

One *instrument* is the unit of work: one input device, one configuration, one
engine instance and one MIDI endpoint. Instruments do not share state.

## Packages

All packages live under `internal/`; HIDI does not offer a Go API.

| Package | Status | Responsibility |
|---|---|---|
| `cmd/hidi` | exists | Command line entry point. Parses arguments and calls into `internal/`. |
| `buildinfo` | exists | Version of the running binary. |
| `assets` | exists | Files embedded into the binary. |
| `midi` | exists | MIDI message types and encoding (MIDI 1.0 bytes and UMP). No I/O. |
| `control` | exists | Platform-neutral input model: device identity, control codes, events. |
| `engine` | exists | Turns control events into MIDI messages for one instrument. Pure and deterministic. |
| `config` | exists | Configuration schema, versioning, migration, validation. |
| `input` | partial | Input device interface, with one subpackage per platform. The Linux backend (`evdev`) exists. |
| `output` | partial | MIDI output backend interface and the router that connects endpoints to existing ports, with one subpackage per transport. The ALSA sequencer backend exists. |
| `instrument` | exists | Device lifecycle: matches devices to profiles, starts and stops instruments, turns them on and off, and grabs their devices. |
| `backend` | exists | Selects the input and output implementations for the platform. |
| `logging` | exists | `log/slog` handler with coloured output and an in-memory buffer for the UIs. |
| `ipc` | exists | Local control protocol between the running HIDI and its clients. |
| `ui` | planned | Terminal views, tray icon and graphical UI. |

### Dependency rules

- `midi` and `control` import nothing from HIDI.
- `engine` imports only `midi`, `control` and the parsed configuration types.
  It performs no I/O and starts no goroutines, so its behaviour is fully
  covered by unit tests.
- Platform-specific code is confined to subpackages of `input`, `output` and
  `ui`. `backend` chooses among them with build constraints and is the only
  other package that may use `//go:build` or import a platform package. The
  one exception is `logging`, which has to switch on colour support in the
  Windows console.
- `instrument` is the only package that wires devices to engines and
  outputs. It sees them through the interfaces of `input` and `output`.

## Design decisions

### Input model

Every control reports a value from 0 to 1. A digital key is a control that
only reports 0 and 1. Sticks, triggers, pressure-sensitive buttons and analog
keyboards use the same event type, so the engine has one binding model for all
of them.

Controls are named with the Linux input event names (`KEY_A`, `BTN_SOUTH`,
`ABS_X`). They identify physical positions rather than characters, which is
what an instrument needs. Other platforms translate their native codes to these
names in the input backend.

### Press-time resolution

A binding is resolved when a control is pressed, and the release always undoes
exactly what the press did. Changing octave, channel, mapping or layer while
controls are held therefore never leaves a note hanging. The same rule applies
to continuous controllers: the engine remembers what it last sent and resets it
when the instrument is disabled or its device disconnects.

### MIDI messages

Messages are typed values with full-resolution fields. Output backends encode
them as MIDI 1.0 bytes or as Universal MIDI Packets. HIDI 2.0 generates
MIDI 1.0 channel voice messages; the representation leaves room for MIDI 2.0
resolution later.

### Outputs

Each instrument writes to one output. The default on every platform is a
virtual MIDI endpoint created for that instrument:

- Linux: an ALSA sequencer port, implemented in Go against the kernel
  interface.
- Windows: a virtual device created through the in-box Windows MIDI Services
  API (`Windows.Devices.Midi2`), which requires Windows 11 25H2 or later.

An instrument can also be connected to existing MIDI ports. That covers a
single-board computer bridging to a hardware MIDI interface or acting as a USB
MIDI gadget. The virtual endpoint stays in either case: a connection is a
subscription from the endpoint to the port, the same thing `aconnect` makes.
`output.Router` keeps the chosen connections in place by looking at the list
of ports once a second, so an interface that is plugged in later, or again,
is connected when it appears.

Network and Bluetooth MIDI outputs are planned after 2.0 and will be further
implementations of the same interface.

### Taking input away from the operating system

While an instrument is active its device does not type or click.

- Linux: the device is grabbed exclusively through evdev.
- Windows: there is no per-device grab. While HIDI is active, all keyboard
  input is suppressed with a low-level hook and read per device through
  Raw Input. Game controllers are read without suppression.

A global key combination, handled inside HIDI's own input path, toggles this
state so that a user with a single keyboard can always get it back. HIDI
refuses to grab keyboards when no combination is set.

A grab takes effect only once no key of the device is down. A program that
saw a key go down and then loses the device never sees it come up and treats
it as held, which would otherwise happen to the Enter key that starts HIDI.

### Configuration

Configuration files are TOML and carry a schema version. A file written for an
older schema is migrated on load. HIDI 1.x configuration files are not
supported.

Profiles come from two places: built into the binary, and files in the
user's profile directory, which take precedence. The directory is polled once
a second and loaded again when it changes; polling keeps this code free of
platform-specific file watching. A changed profile reaches a running
instrument through `Manager.SetProfiles`, which replaces the engine and keeps
the MIDI endpoint. A device that no profile selects stays open but is read
only twice a second, so that a profile added later can still claim it without
HIDI following every movement of a mouse.

### Logging

Every component logs through `log/slog` to one `logging.Log`, which writes
lines to the terminal and keeps the newest in memory, so that an interface
can show the log without reading a file. The output and the memory have
separate levels: an interface can keep debug lines while the terminal stays
quiet.

A line is `date time LEVEL [component] message key='value' ...`, coloured
when the output is a terminal. `-no-color`, `NO_COLOR` and `FORCE_COLOR`
override that; plain output is always available.
The names devices report end up in the log, so everything that is not
printable text is escaped: a device cannot forge a log line or send escape
sequences to the terminal.

### Processes

`hidi run` runs the instruments in the foreground. The terminal views
(`hidi status`, `hidi monitor`) and the graphical UI connect to it over a
local control socket, so closing a window never stops the instruments.

The socket is a Unix domain socket, which Windows has as well, in a directory
only the user can enter: `$XDG_RUNTIME_DIR/hidi/control.sock` on Linux. The
protocol is one JSON object per line and is described in the `ipc` package. A
client asks for the state once, switches HIDI on or off, or watches: it then
receives a snapshot after every change, at most twenty a second, and the log
lines as they are written.

The socket also makes HIDI a single instance. A second `hidi run` finds the
first one answering and stops before it opens any device.
