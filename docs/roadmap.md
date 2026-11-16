# HIDI 2.0 roadmap

This file records what was agreed for HIDI 2.0, what is done, and what
remains, in the order it is planned. Update it in the pull request that
completes or changes an item.

## How the work is organised

- `main` holds the released 1.x code and is not touched until 2.0 is ready.
- `dev` is the integration branch. Nothing is committed to it directly.
- Work happens on a feature branch per change, merged into `dev` through a
  pull request with a merge commit, so that the commits and their messages
  are preserved. The pull request description becomes the merge commit
  message.
- One pull request is open at a time. The next stage starts when the
  previous one is merged.
- `dev` does not have to be usable at every commit, but CI must pass.

## Decisions

| Topic | Decision |
|---|---|
| Language | Go, with no cgo in the core and the headless build. |
| Platforms | Linux and Windows 11 (25H2 or later). macOS is out of scope for 2.0. |
| Devices | Every input device is one independent instrument with its own MIDI device, which appears and disappears with the hardware. Merging several devices into one instrument is out of scope. |
| MIDI | MIDI 1.0 messages. Values are kept at full resolution internally so that MIDI 2.0 output can be added later. HIDI has its own MIDI code and no MIDI library. |
| Linux output | ALSA sequencer, through the kernel interface. A virtual device is the default; sending to an existing port stays possible for single-board computers. |
| Windows output | Windows MIDI Services through the in-box `Windows.Devices.Midi2` API, with no fallback. Until Microsoft ships the API in Windows, users install its preview. A small C++ helper DLL is acceptable if calling the API from Go proves impractical. |
| Windows input | While HIDI is on, all keyboard input is suppressed, and devices are told apart through Raw Input. Suppressing one keyboard and not another would need a driver and is left for later. |
| Grabbing | Recognised devices do not reach the rest of the system while HIDI is on. Game controllers are the exception unless the user asks otherwise. |
| Configuration | TOML, with a schema version and migrations from 2.0 onwards. HIDI 1.x files are not read. |
| Configuring | The graphical interface is the first-class way to configure everything; nobody should have to edit a file. Files can still be edited by hand, and every mistake in one is reported clearly. |
| Interfaces | A terminal interface for headless use, a tray icon, and a graphical interface built with raylib. They are clients of the running engine. |
| Graphical interface | Draws at the monitor's refresh rate (vertical sync), including high refresh rates, with an optional, configurable lower limit while its window is not focused. It never keeps the system or the display awake: it must not reset the operating system's idle timer. |
| Axes on a channel or layout change | Both behaviours are needed, chosen by a setting: an axis either keeps the value of the controller it left, or that controller returns to rest. |
| Releases | Release artifacts are immutable, and builds are reproducible: the same commit gives the same binaries. |
| Logging | Structured, coloured log lines: time, level, component, message, and `key='value'` fields. |
| OpenRGB | Supported through a server the user runs. HIDI does not bundle one. |
| Go version | The current release; 1.27 at the time of writing. |

## Done

- Module layout, CI for Linux and Windows, and the architecture document.
- `midi`: messages and their MIDI 1.0 and UMP encodings.
- `control`: the platform-neutral model of controls, events and devices.
- `engine`: bindings, layouts, key combinations, note tracking, collision
  modes, commands, and returning controllers to rest.
- `config`: the profile file format with versioning, and the built-in
  profiles converted from 1.x.
- `output/alsaseq`: virtual MIDI devices on Linux.
- `input/evdev`: reading Linux input devices, discovery, hot-plugging,
  merging the nodes of a device, and grabbing.
- `instrument`: running instruments, turning HIDI on and off with a key
  combination, and grabbing keyboards.
- `logging`: the coloured, structured log format, and the newest lines
  kept in memory for the interfaces.
- `hidi run` and `hidi devices`.
- CI runs the tests that need kernel interfaces in a virtual machine.

## Remaining

In planned order. Each item is roughly one pull request.

### Linux

1. **User profiles.** Loading profiles from the user's configuration
   directory, reloading them when they change without restarting the
   instruments that are not affected, and a way to write the built-in
   profiles out as a starting point. A broken file is reported with what
   is wrong and where, and never stops the other profiles from loading.
2. **Sending to an existing MIDI port.** Listing ALSA ports and connecting
   an instrument to one, for hardware MIDI interfaces and USB gadget mode.
3. **Control interface.** A local protocol to a running HIDI, with
   `hidi status`, `hidi on`, `hidi off` and a live `hidi monitor` view.
4. **Rate limiting of axis input**, which 1.x had at 120 updates a second,
   and the setting for what an axis does on a channel or layout change.
5. **An end-to-end test** from a virtual input device to a received MIDI
   message, in the CI virtual machine.

### Windows

6. **MIDI output** through Windows MIDI Services. This starts with a small
   test program to prove that a virtual device can be created from Go.
7. **Input**: Raw Input for keyboards and game controllers, and the
   low-level hook that suppresses keyboard input while HIDI is on.
8. **Platform glue**, and testing on a real machine.

### Interfaces

9. **Tray icon** with the on/off switch.
10. **Graphical interface**: status of the instruments, a live view of what
    is played, and the log. Frame pacing as decided above.
11. **Configuring in the graphical interface**: every setting and every
    part of a profile, including learning a binding by pressing the
    control. This needs the configuration to be written back to its file
    without losing what the user wrote there by hand.

### Features

12. **Chord memory** (the 1.x "multinote" action, which computed its
    intervals but never applied them) and **CC learning**.
13. **MIDI input and OpenRGB**: lighting the keys that are played here or
    by another MIDI source.
14. **Velocity from how a control is pressed**, for controls that report
    pressure or travel.

### Release

15. Packaging, a udev rule so that HIDI needs neither root nor group
    changes, and updating the dependencies of the CI workflow.
16. The release workflow: reproducible builds (`-trimpath`, a pinned
    toolchain, no build time in the binary), checksums, build provenance
    attestation, and immutable releases.
17. Rewriting the README and the user documentation.
18. Merging `dev` into `main`.

### After 2.0

- Bluetooth and network MIDI output, so that a single-board computer can
  present itself to another machine without a cable.
- Analog keyboards, through the vendors' SDKs.
- MIDI 2.0 messages.
- macOS.
- An arpeggiator and a sequencer.

## Not yet checked on real hardware

Everything so far was tested with devices created in software. These points
rest on assumptions about real devices and should be checked first:

- That the MIDI devices appear and work in a DAW, also through PipeWire and
  JACK.
- That the media keys of a keyboard arrive together with its other keys.
- That a mouse which also presents a keyboard is left alone.
- Which axes of the Saitek X56 stick and throttle return to their centre;
  the built-in profiles guess.
- That the toggle combination suits a keyboard with one Ctrl and no
  right-hand modifiers, and does not collide with a desktop shortcut.

## Open questions

- Where the setting for axes on a channel or layout change belongs: one
  value for a profile, or one for each binding. Today an axis that is not
  marked as centred always keeps its controller value.
- Whether the settings of HIDI itself (the toggle combination, grabbing,
  the frame rate limit) live in one file next to the profiles.
