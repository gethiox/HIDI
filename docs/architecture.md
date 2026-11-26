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
| `assets` | exists | Files embedded into the binary. `assets/icon` holds the shapes of HIDI's icon, from the drawing `hidi.svg` beside it, and fills them at any size in the colours of a state: for the tray, the window and the Windows program file. |
| `update` | exists | Asks GitHub for the newest release and compares versions. Only the window uses it, and only where the settings allow. |
| `midi` | exists | MIDI message types and encoding (MIDI 1.0 bytes and UMP). No I/O. |
| `control` | exists | Platform-neutral input model: device identity, control codes, events. |
| `engine` | exists | Turns control events into MIDI messages for one instrument. Pure and deterministic. |
| `config` | exists | Configuration schema, versioning, migration, validation. |
| `input` | partial | Input device interface, with one subpackage per platform. The Linux backend (`evdev`) exists. The Windows backends (`rawinput` for keyboards and most game controllers, `xinput` for Xbox controllers) await their first run on a real machine. |
| `output` | partial | MIDI output backend interface and the router that connects endpoints to existing ports, with one subpackage per transport. The ALSA sequencer backend exists; the Windows MIDI Services backend is written and awaits its first run on a real machine. |
| `instrument` | exists | Device lifecycle: matches devices to profiles, starts and stops instruments, turns them on and off, and grabs their devices. Before the engine it thins out axis input and drops the trembling of resting axes; it counts what comes in and goes out. |
| `backend` | exists | Selects the input and output implementations for the platform. |
| `logging` | exists | `log/slog` handler with coloured output and an in-memory buffer for the UIs, and the footer that keeps lines at the bottom of a terminal while the log runs above. |
| `light` | exists | What the keys of a keyboard show for the state of its instrument, and a hub that sends it to the keyboard's lights through an OpenRGB server (`light/openrgb` is the client). |
| `settings` | exists | HIDI's own settings, where HIDI keeps its files, and filling that directory on the first start. |
| `ipc` | exists | Local control protocol between the running HIDI and its clients. |
| `ui` | partial | The interfaces of the desktop. `ui/tray` is the icon in the notification area and `ui/gui` the window. The terminal views live in `cmd/hidi`. |

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
  API (`Windows.Devices.Midi2`), which requires Windows 11 25H2 or later. The
  `winmidi` package calls this WinRT API without cgo, through the interface
  tables. Until Windows ships the API's DLL, which Microsoft announces for
  2027, the preview copy of `Windows.Devices.Midi2.dll` has to sit beside
  `hidi.exe`; the package asks Windows first and falls back to that copy.
  `docs/research/windows-midi-abi.md` records the interface facts and where
  each comes from.

An instrument can also be connected to existing MIDI ports. That covers a
single-board computer bridging to a hardware MIDI interface or acting as a USB
MIDI gadget. The virtual endpoint stays in either case: a connection is a
subscription from the endpoint to the port, the same thing `aconnect` makes.
`output.Router` keeps the chosen connections in place by looking at the list
of ports once a second, so an interface that is plugged in later, or again,
is connected when it appears.

Network and Bluetooth MIDI outputs are planned after 2.0 and will be further
implementations of the same interface.

An output can break while HIDI runs: on Windows, a virtual device lives in
the MIDI service and is gone when that service's process ends. An
instrument whose send fails lets go of its output, gives its keys back to
the system and tries for a new output, after two seconds and then after
twice as long each time, up to thirty. An output may also say that it is
lost without anything being sent (`output.Checker`), and an instrument asks
every two seconds; the Windows output answers from the process the MIDI
service runs in, which it looks at once a second. A call into Windows that
does not return is given up after a time, and nothing more is asked of the
service for a while that grows from ten seconds to five minutes.

### Lights and MIDI input

`light.Paint` turns the state of an instrument into a colour for every key:
it is a pure function of what the keys do in the selected layout, the
instrument's state, the notes that sound and whether HIDI is on. A hub owns
the connection to OpenRGB, finds for each instrument the keyboard among the
server's controllers, and sends colours only when they changed.

A keyboard takes some tens of milliseconds to be written, and how OpenRGB
deals with updates that come faster differs between its versions. Released
ones note that there is something to write and forget the note when the
write in progress ends, so an update that arrives during a write is lost
until the next one. Newer ones take an update only when the write before is
done, so updates queue up in the connection and the lights fall behind. The
hub is safe against both: it sends a change at once, then asks the server a
question that is answered in order and waits for the answer before it sends
the next change, which is then the newest scene; and it sends what it sent
last a few more times over the following second. What the hub last sent is
also what the window draws as the glow of the keys. OpenRGB says where it reaches a keyboard, not which
input device that is, so the two are matched by model and, on Linux, by the
port the system reports for both.

HIDI does not talk to keyboards' lights itself. OpenRGB knows hundreds of
them and is what people who care about their lights run already; it stays a
separate program, reached over its network protocol.

So that the lights can show notes from elsewhere, an instrument's MIDI
device can be one that other programs also send to (`output.Receiver`). It
is only opened that way when the lights are on, because a device that
accepts MIDI has to keep reading it. What arrives is only looked at for
which notes are on; it is not passed on anywhere.

### Taking input away from the operating system

While an instrument is active its device does not type or click.

- Linux: the device is grabbed exclusively through evdev.
- Windows: there is no per-device grab. While HIDI is active, all keyboard
  input is suppressed with a low-level hook. Keys that pass are read per
  device through Raw Input. A key the hook swallows is not reported there,
  so the hook hands it to HIDI itself; the hook does not know the keyboard,
  and the key goes to the keyboard that Raw Input last saw typing
  (`rawinput.claims`). Windows has no way to keep a game controller from
  other programs, so `--grab-gamepads` has no effect there.

A profile can leave single keys to the system while the others play
(`"passthrough"`). The device is then shared instead of grabbed
(`input.Passer.Share`): on Windows the hook lets those keys through; on
Linux, where a device is grabbed whole or not at all, HIDI types them again
on a keyboard of its own, as it does for `always_grab`. Which keys are open
follows the selected layout.

Game controllers on Windows are read in two ways, because no single
interface serves all of them well:

- Xbox controllers, and the many that imitate one, through XInput
  (`input/xinput`). Windows also presents them as HID devices, for older
  programs, but deliberately incomplete there: both triggers share one
  axis. XInput has no notifications, so HIDI asks it for the state of each
  connected controller every few milliseconds, and once a second whether
  another appeared. It knows four controllers at most.
- Every other controller, old ones included, from its HID device
  (`input/rawinput`): the controllers are found in the list Raw Input
  keeps, looked through once a second, and each is then opened and read
  by a goroutine of its own, which gets the HID reports exactly as the
  controller sends them; `hid.dll` takes the buttons and axes out of a
  report. They are not asked for through Raw Input itself: Windows takes
  input it delivers that way for somebody using the machine, so a stick
  that reports all the time, as one with a little play does, kept the
  screen from turning off for as long as HIDI ran. Values are told apart by the index Windows gives each, not by
  their usage, since a stick with four hats has four values of one usage.
  The same way also reads Xbox controllers, with the shared trigger
  axis split in two, should XInput be missing.

The newer interfaces of Windows for controllers were not chosen: they
deliver input only to the program whose window is in front, and an
instrument must keep playing while the DAW is.

A controller names its controls in the terms of the HID standard, or XInput
names them. Both are translated to the codes the Linux kernel gives the same
controller, so that one profile fits both systems: by the kernel's generic
rules for most, and by a table for Xbox and PlayStation controllers, which
have drivers of their own on Linux.

A global key combination, handled inside HIDI's own input path, toggles this
state so that a user with a single keyboard can always get it back. HIDI
refuses to grab keyboards when no combination is set.

A grab takes effect only once no key of the device is down. A program that
saw a key go down and then loses the device never sees it come up and treats
it as held, which would otherwise happen to the Enter key that starts HIDI.
On Windows the hook reaches the same end differently: it lets the release of
a key through if other programs saw its press, and swallows a key to its
release if they did not.

On Windows a hidden window on a thread of its own receives the raw input and
runs the hook. That thread never waits for the rest of HIDI, because Windows
removes a hook whose thread is slow to answer; events leave it through a
queue that grows as needed.

### Configuration

Configuration files are TOML and carry a schema version. A file written for an
older schema is migrated on load. HIDI 1.x configuration files are not
supported.

Everything the user can change is in one directory named HIDI in the
system's place for configuration, which `settings` knows: HIDI's own
settings in `settings.toml`, and the profiles as files in `profiles`. The two
are kept apart on purpose: a profile says how one kind of device plays, the
settings say how HIDI behaves.

What HIDI notes for itself is kept apart from both, in a state directory
(`%LocalAppData%\HIDI`, `$XDG_STATE_HOME/HIDI`): the record of the files
it wrote, by which it knows a directory for its own, and how the window
was left. On Windows the settings follow the user between machines and
this does not. Nothing is kept about the devices HIDI has met.

HIDI carries its default settings and profiles inside the binary. The
profiles are written into `profiles/factory` and kept there exactly as the
binary has them, on every start: missing ones written, differing ones
replaced, ones HIDI no longer has removed. That directory is HIDI's, and
its files are marked read-only. The user's profiles are in `profiles/user`,
which HIDI creates and never writes to by itself; a profile there comes
before a factory profile that fits a device equally well, and can say
`enabled = false` to be left out. Keeping the two apart is what lets a new
HIDI improve its profiles without asking whether a file was changed, and
lets the window show which is which. The copies inside HIDI are used
directly only without a directory.

The first HIDI 2 builds kept everything in `profiles` itself and recorded a
fingerprint of every file they wrote. Such a directory is converted once:
a file that still has its fingerprint was never changed and is dropped, any
other moves to `profiles/user`. A directory without that record and with
anything HIDI 2 does not keep there is never written to.

The profile directory is polled once
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
when the output is a terminal. `--no-color`, `NO_COLOR` and `FORCE_COLOR`
override that; plain output is always available.
The names devices report end up in the log, so everything that is not
printable text is escaped: a device cannot forge a log line or send escape
sequences to the terminal.

### When HIDI dies

A crash needs no cleaning up. A grabbed keyboard returns to the system when
the process that grabbed it ends, on Linux with its file handle and on
Windows with its hook. The MIDI devices belong to the process as well and are
removed with it.

What a crash leaves is its reason. The Go runtime prints it to the terminal,
and `crashlog` has it written to a file in the user's cache directory too; the
next `hidi run` says that the last one did not end properly and names the
file. A crash cannot be turned into a friendlier message while it happens: a
failure in a goroutine other than the command's own, or inside a call into
the operating system, ends the process without running any more of its code.

Calls that leave the process are limited in time instead. A MIDI service that
does not answer is reported and the endpoint abandoned, so that HIDI itself
stays usable and can be stopped.

### Processes

`hidi` without a command is the application: one process runs the
instruments and shows the icon in the tray, and starts the window as a
second process when it is asked for. `hidi --headless`, or `hidi run`, is
the same process without icon and window. The terminal views (`hidi
status`, `hidi monitor`) and the window connect to it over a local control
socket, so closing a window never stops the instruments.

The window is a process of its own for two reasons: whatever a graphics
driver does stays away from the instruments, and a closed window gives all
its memory back. Started while HIDI runs, `hidi` finds the socket answered
and opens the window of the HIDI that runs. Where the desktop shows no
icons, the window is opened in place of the icon and closing it ends HIDI.
On Windows a release is built as a program without a console
(`-H windowsgui`), so that a double click opens no console window. Such a
program starts with nowhere to write; `findConsole` joins the console of
the command line that started it, if there is one, and gives the standard
streams that lead nowhere to that console, leaving redirected ones alone.
`--console` opens a console of HIDI's own. The price is that a command
line does not wait for such a program. A build without the linker flag is a
console program as before, and leaves the console Windows gave it when
started by a double click.

The socket is a Unix domain socket, which Windows has as well, in a directory
only the user can enter: `$XDG_RUNTIME_DIR/hidi/control.sock` on Linux. The
protocol is one JSON object per line and is described in the `ipc` package. A
client asks for the state once, switches HIDI on or off, or watches: it then
receives a snapshot after every change, at most twenty a second unless it
asks for another pace, and the log lines as they are written. The window
asks for one every four milliseconds, so that an axis moves as smoothly as
the monitor can show.

The icon is the same code in two places. In the application it follows the
HIDI of its own process, offers the window and ends HIDI. `hidi tray` is a
client like the others: an icon for a HIDI that runs headless, which can
say "not running" and whose removal leaves HIDI running. On Linux it speaks the StatusNotifierItem protocol and
offers its menu as a `com.canonical.dbusmenu` object, both over D-Bus, with
the pure Go library `godbus`. On Windows it uses `Shell_NotifyIcon`. The
icon is drawn by the program in every size asked for, so there are no image
files to install.

`hidi gui` is the window, also a client. It is drawn with raylib through
the binding `raylib-go`, which loads raylib as a shared library it carries
with it, without cgo. That works on 64-bit Linux and Windows; elsewhere, and
in a build with cgo turned on, which would compile raylib from C instead,
`hidi gui` says that there is no window. The rest of HIDI is the same in
every build.

The window follows the decisions on the roadmap:

- It is drawn in step with the monitor it is on, at whatever rate that
  refreshes, and only when something it shows has changed: the state or
  log of HIDI, or the pointer. A lower limit can be set, and another for
  a window that is not in front.
- The settings are a sheet over the instruments, which stay in view and
  take no input while it is open.
- Nothing in it asks the system to stay awake.
- Text is set in DejaVu Sans, embedded in the program. Everything that
  changes while one looks at it, the numbers of an instrument and the log,
  is set in DejaVu Sans Mono with a fixed number of places, so that
  nothing moves sideways.
- The log is the bar at the bottom: its last line, coloured as in the
  terminal but faint, and the whole log when the bar is pulled up, with a
  choice of the least severe level to show.

To show what is played, the state HIDI reports for an instrument names the
notes that are on. To show what the device itself does, it also lists the
buttons and axes of the device and where each stands; `instrument` keeps
that list. A game controller says what it has when it is opened
(`input.Describer`): the kernel's lists on Linux, the HID capabilities and
the fixed set of XInput on Windows. The list is seeded with that, so the
window shows and numbers every control from the start, and it says which
axes are those of a hat. An axis that was only listed has no position yet,
and is not among those sent again when an instrument starts playing.

The same list is what a profile's `[generate]` rule works on. The engine
knows nothing of rules: `config.Profile.For` takes the controls of a device
and returns an engine profile in which every layout has, beside the
bindings the file names, one for each control that neither the layout nor
`[global]` names. The manager builds a device's engine from that. Since the
rule sees nothing but the device's own list, in the order of its codes, the
same device gets the same instrument on every start, and the window can
work out for itself what the running HIDI plays.

What turns HIDI off and on is a command of the profile, `hidi_toggle`, and
the one command the engine does nothing for: the engine does not run while
HIDI is off, which is when the key matters most. The parser lifts the keys
of that binding out of the profile (`config.Profile.Toggle`), and the
manager makes them the rule for each device that plays with the profile,
unless the settings name one toggle for all. The loop of the device watches
the keys in both states, as it always did. A keyboard for which the rule
comes out empty is not grabbed.

The settings in use are the running HIDI's (`liveSettings` in `cmd/hidi`),
not the file's and not the window's. They start as the file's. The window
hands over what its sheet shows, change by change, and asks for them when
it opens; a file changed by hand is taken over in what it changed, compared
with how it was last read. Saving in the window writes the file and nothing
more, so that a write is never a reason for anything to be applied.

What moves in the window moves by time, not by frames. The curves are
placed by how old the newest step was when HIDI reported it plus the time
since that report arrived, so a frame that comes late draws them where
they are by then. A frame limit that is the monitor's rate divided by a
whole number is paced at the middle between two of the monitor's pictures,
so that the wait for the monitor lands every frame on the same picture and
they come at even distances (`FrameRate`).

A profile is changed from the window in its text. `config.Set` and
`config.Remove` find the line of one key in one section and change that
line; everything else in the file stays byte for byte, the notes a person
wrote there among it. They know the shapes a profile has, not all of TOML:
text written in a way they do not follow is left alone and the window says
so. The result is parsed as a profile before it is written, so the window
cannot write a file HIDI would refuse. The running HIDI learns of the
change like of any other, by watching the directory.

Whether a page has something to save is asked of the profiles, not of the
text: `config.Same` compares what is shown with what the file says as
parsed profiles, setting aside the order of lines. A change that is taken
back by hand leaves nothing to save.

The page names what it changes by a `Spot`: a control, or one half of an
axis. One function writes a role out as the right side of a binding, and
every change goes through it.

What the page shows of the device at work comes from an engine of its own
(`gui.Shadow`): the same `engine.Engine`, made of the profile as it is
shown and the one layout that is shown, fed with the buttons and axes of
the snapshot. The page asks it what is held, what each control sends and
where each axis rests. There is no second reckoning of dead zones, trigger
points or hysteresis for the eye, and what is shown is right while HIDI is
off as well.

The window's own widgets are a small kit over raylib (`kit` in `ui/gui`):
buttons, switches, fields and steppers that take the pointer and the scale
of the interface from one place, a layout helper that wraps rows, an
overlay for a question that has to be answered before anything else is
touched, and a hint that appears beside a widget the pointer rests on. The
hint is there for any widget to use; no text has been given to one yet.

The socket also makes HIDI a single instance. A second HIDI finds the first
one answering and stops before it opens any device, or opens the first
one's window.
