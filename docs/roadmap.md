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
- Every pull request for 2.0 carries the label `v2.0`.
- A pull request is a whole area that can be tried in one go, not a small
  step towards one: the remaining work is planned as about six of them
  (see "Remaining"). While one is being written, pictures of the state so
  far go to the maintainer before it is opened.
- A pull request holds one topic. A fix for something else, found while
  it is open, goes into a pull request of its own, also when that means
  two are open at once. Whatever a pull request contains is described in
  its text.
- `dev` does not have to be usable at every commit, but CI must pass.

## Decisions

| Topic | Decision |
|---|---|
| Language | Go, with no cgo in the core and the headless build. |
| Platforms | Linux and Windows 11 (25H2 or later). macOS is out of scope for 2.0. |
| Devices | Every input device is one independent instrument with its own MIDI device, which appears and disappears with the hardware. Merging several devices into one instrument is out of scope. |
| MIDI | MIDI 1.0 messages. Values are kept at full resolution internally so that MIDI 2.0 output can be added later. HIDI has its own MIDI code and no MIDI library. |
| Linux output | ALSA sequencer, through the kernel interface. A virtual device is the default; sending to an existing port stays possible for single-board computers. |
| Windows output | Windows MIDI Services through the in-box `Windows.Devices.Midi2` API, with no fallback. Microsoft announces the API's DLL as part of Windows from 2027; until then the preview copy of `Windows.Devices.Midi2.dll` sits beside `hidi.exe`. A small C++ helper DLL is acceptable if calling the API from Go proves impractical. |
| Windows input | While HIDI is on, all keyboard input is suppressed, and devices are told apart through Raw Input. Suppressing one keyboard and not another would need a driver and is left for later. |
| Grabbing | Recognised devices do not reach the rest of the system while HIDI is on. Game controllers are the exception unless the user asks otherwise. |
| Configuration | TOML, with a schema version and migrations from 2.0 onwards. HIDI 1.x files are not read. |
| Where things are kept | One directory named HIDI in the system's place for configuration (`~/.config/HIDI`, `%AppData%\HIDI`): `settings.toml` for HIDI's own settings, apart from the profiles in `profiles/`. HIDI carries its defaults inside and writes them there on the first start, and only into a directory that is new or its own. |
| Writing files | A file HIDI changes is changed in the text, one key at a time, never written anew from what HIDI understood of it. The new text goes to a file beside the old one, is brought to the disk, and takes its place in one step, so that a power failure leaves the old file or the new one. |
| Configuring | The graphical interface is the first-class way to configure everything; nobody should have to edit a file. Files can still be edited by hand, and every mistake in one is reported clearly. |
| Interfaces | A terminal interface for headless use, a tray icon, and a graphical interface built with raylib. They are clients of the running engine. |
| Graphical interface | Draws at the monitor's refresh rate (vertical sync), including high refresh rates, with an optional, configurable lower limit while its window is not focused. It never keeps the system or the display awake: it must not reset the operating system's idle timer. |
| Axes on a channel or layout change | Both behaviours are needed, chosen by the profile's `axis_on_change`: an axis either keeps the value of the controller it left, or that controller returns to rest. |
| Fonts | DejaVu Sans, and DejaVu Sans Mono for the log and for every text that changes while it is watched, padded to a fixed width so that nothing around it moves. The files and their licence are in `internal/assets/fonts`. |
| Reviewing interfaces | A pull request that changes what an interface looks like includes screenshots, rendered in software where there is no display. |
| Releases | Release artifacts are immutable, and builds are reproducible: the same commit gives the same binaries. |
| Logging | Structured, coloured log lines: time, level, component, message, and `key='value'` fields. Output without colours is always available. HIDI logs little, since the interfaces show its state; what it logs has a fitting level and its context as fields. |
| OpenRGB | Supported through a server the user runs. HIDI does not bundle one. |
| Go version | The current release; 1.27 at the time of writing. |

## Current focus

Windows, tried on the maintainer's machine (Windows 11 build 26300,
`Windows.Devices.Midi2.dll` from Microsoft's Preview 11 NuGet package
beside `hidi.exe`).

What is known:

- `hidi doctor` creates one MIDI device, and Bitwig lists it and plays the
  notes. Removing it by disconnecting never returned.
- `hidi run` finds keyboards and game controllers, an Xbox controller
  through XInput among them. With ten devices it created no MIDI device:
  every one ran into its time limit at "creating the device", all at the
  same moment.

What was changed for that, and is to be tried next with `hidi doctor`,
which now makes three devices, removes them, and makes one more:

1. MIDI devices are created and removed one at a time. Whether the ten at
   once were the cause, or a MIDI service left stuck by an earlier removal
   that hung, is what the next run tells.
2. A device is given up by closing its session, without disconnecting
   first. `hidi doctor -removal disconnect` and `-removal release` try the
   other ways.
3. Once a call to the MIDI service did not return, nothing more is asked
   of it, so that HIDI says so once and at once.
4. Every instrument's MIDI device has a name of its own; the second
   device of a model gets a number.
5. The interfaces of one piece of hardware are one device: they are told
   by the container Windows puts them in. Before, a keyboard with several
   interfaces was listed several times.

Still open there: a mouse that also presents a keyboard interface is
listed as a keyboard; after a HIDI that was killed, whether the next one
can create its devices; keyboard input itself, which could not be tried
while no instrument started.

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
- Profiles of the user: read from the configuration directory, applied to
  running instruments when a file changes, and `hidi profiles` to list,
  check and export them.
- Connecting instruments to existing MIDI ports on Linux, with `hidi ports`
  and `hidi run -connect`.
- The control interface: a local socket to the running HIDI, with
  `hidi status`, `hidi on`, `hidi off`, `hidi toggle` and `hidi monitor`.
- Axis settings in a profile: the rate at which an axis is passed on, and
  what its controller does on a channel or layout change.
- An end-to-end test in the CI virtual machine: the real program, a
  keyboard created in software, and a MIDI receiver.
- MIDI input on Linux (`alsaseq.Input`), which the test uses as its
  receiver and which lighting the played keys will build on.
- `hidi run` and `hidi devices`.
- The window shows the raw input of each device: a full-size keyboard
  with the keys that are down, or the list of buttons and axes a game
  controller has reported.
- Chord memory (the `chord` command; the 1.x "multinote", which computed
  its intervals but never applied them) and the `learn` command, which
  holds back axes that are not moved far (the 1.x "cc_learning").
- Changing a profile in the window: its starting numbers, and what each
  key or button does (a note, a command, nothing), chosen by a click or
  by pressing it on the device. Every change is one line of the file;
  what was written there by hand stays.
- The settings page of the window: a gear in the corner that turns while
  the page is open and keeps its angle; the settings, with the switches and
  the frame limit changeable; where things are kept, with buttons to open
  the places in the file manager; and the list of all profiles.
- Velocity from how fast a control is pressed (`touch = true` on a note),
  for triggers, analog keys and pads.
- Lighting the keys through OpenRGB: what every key plays, the state of
  the command keys, the notes that sound, and notes sent to the instrument
  by another program, for which its MIDI device accepts MIDI on Linux.
- A running HIDI follows its settings file: a changed toggle combination
  and changed grabbing apply within seconds, from the window or by hand.
- HIDI's directory with the settings file and the profiles, filled on the
  first start, and `hidi config` to show where it is.
- The window (`hidi gui`): the instruments with their state, a keyboard
  that shows the notes being played, the on/off switch, and the log in a
  bar at the bottom that can be pulled up, with a level filter.
- The icon in the notification area (`hidi tray`), for Linux and Windows:
  shows on, off and not running, turns HIDI on and off, lists the
  instruments.
- CI runs the tests that need kernel interfaces in a virtual machine.

## Remaining

In planned order. Each item is roughly one pull request.

### Windows

1. **MIDI output** through Windows MIDI Services works: on Windows 11
   build 26300 with the DLL of Microsoft's Preview 11 package,
   `hidi doctor` created a virtual device and sent to it. Every method
   position that creating and sending uses is thereby confirmed for that
   version, and a DAW received the notes. Still open: removing the
   device, where disconnecting does not return and runs into its time
   limit.
2. **Input**: Raw Input for keyboards and the low-level hook that
   suppresses keyboard input while HIDI is on are written
   (`internal/input/rawinput`) and have to be tried on a real machine.
   Game controllers are read too, with their controls translated to the
   codes Linux gives them: Xbox controllers through XInput
   (`internal/input/xinput`), every other one through Raw Input. Also
   untried.
3. **Platform glue**, and testing on a real machine.

### Interfaces

6. **The rest of configuring in the graphical interface**:
    - The toggle combination, by pressing the keys, and the MIDI ports to
      connect to, from the list of those that exist. The settings page
      shows them and points to the file. Changed ports need the router to
      drop connections too.
    - In a profile: axes, controllers, program changes, pitch bend, keys
      with modifiers, and the options of a note; adding and removing
      layouts; making a new profile and saying which devices it is for.

### Features

### Release

10. Packaging, a udev rule so that HIDI needs neither root nor group
    changes, and updating the dependencies of the CI workflow.
11. The release workflow: reproducible builds (`-trimpath`, a pinned
    toolchain, no build time in the binary), checksums, build provenance
    attestation, and immutable releases.
12. Rewriting the README and the user documentation.
13. Merging `dev` into `main`.

### After 2.0

- **Steam Controller** (the first one, the new one, and the controls of a
  Steam Deck) on Windows. These speak a protocol of Valve's own instead of
  describing themselves as game controllers, so neither reader sees them
  unless Steam is running and presents them as an Xbox controller. Reading
  them directly means a third reader for that protocol, as the Linux
  kernel has in `hid-steam`; on Linux that driver already makes them
  ordinary controllers. Moved here because it cannot be written without
  the hardware to try it on.

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
- That `hidi run -connect` reaches a real USB MIDI interface, and
  reconnects after it was unplugged.
- That the window of `hidi gui` opens and is sharp on a real display, also a
  dense one, on Wayland and on Windows; that it keeps to the refresh rate
  of a fast monitor; that it lets the system go to sleep. It ran on a
  virtual display with software drawing only.
- That the icon of `hidi tray` shows and works in KDE, GNOME (with the
  AppIndicator extension) and on Windows; it was tried in the panel of Xfce
  only.
- Windows: that Raw Input still delivers the keys the hook swallows; that
  XInput reports the guide button and the model of a controller through
  its two functions without a name; that a DualShock 4 or DualSense numbers its controls
  as the table in `rawinput/pad.go` says, also over Bluetooth.
- The lights with a real OpenRGB server and a real keyboard. The client
  was written from OpenRGB's protocol documentation and tried against a
  stand-in built from the same document: that the server sends what the
  document says, the names it gives the lights of a given keyboard, and
  that it reports the keyboard at a location HIDI can match, are unchecked.
- HIDI beside the real input-remapper (issue #23). Its devices were
  imitated in a test from what its source does: a keyboard taken for
  itself, the device that passes it on, and its own keyboard and game
  controller with one shared identity.
- That the toggle combination suits a keyboard with one Ctrl and no
  right-hand modifiers, and does not collide with a desktop shortcut.

## Open questions

- The colours of the lights are those of 1.x and fixed. 1.x let a profile
  set them; that, and showing an axis on a keyboard's light strip as 1.x
  did for one model, are not carried over yet.
- Receiving MIDI on Windows, for the lights there, needs the event of
  Windows MIDI Services to be handled, which HIDI does not do yet.
- The lights are switched on at the start of HIDI only, and not from the
  window.

- On Windows the directory is in the roaming `%AppData%`, the usual place
  for settings that follow the user. The local one would be
  `%LocalAppData%`; say so if that is preferred.
- A directory `~/.config/hidi` or `%AppData%\hidi` that a build of 2.0
  before the settings file used is not looked at any more.

- The built-in game controller profiles bind neither `chord` nor `learn`;
  1.x had learning on the left shoulder button, which is a note now.

- The icon is a program the user has to start. Whether HIDI should put it
  among the programs started at login, and whether `hidi run` should start
  it, is to be decided with packaging.

- `axis_on_change` is one setting for a profile. Whether single bindings
  need to differ from it will show in use.
- Windows, opt-in: suppress all keyboard input in the hook, and type the
  keys of keyboards that are not instruments back in with `SendInput`
  once Raw Input has said which device they came from. To be tried in the
  Windows input stage; the known costs are that injected keys do not reach
  elevated windows and that some games reject them.
- How HIDI for Windows is distributed until the MIDI API is part of
  Windows. The API's DLL to build and test against is the one in
  Microsoft's NuGet package `Windows.Devices.Midi2`, version
  0.99.92-preview.11 at the time of writing, from the releases of
  github.com/microsoft/MIDI; the copy their Tools installer puts next to
  its own programs must not be used. Shipping the DLL with HIDI means
  accepting Microsoft's "go-live" terms from those release notes: telling
  users that a preview API is in use, keeping up with new previews,
  removing the DLL once Windows has it, and not shipping to Windows below
  11 25H2. Whether a GPL program may carry that DLL in its download is a
  licence question to settle before the first release; the alternative is
  to have users fetch the DLL themselves.
- The identifier a virtual device declares on Windows: with spaces in it
  the MIDI service refused to create the device, without a reason; made
  of letters, digits and dashes it worked. That the spaces were the
  cause is likely but was not isolated.
- Two input devices of the same model get MIDI devices of the same name.
  They need to be told apart, in the name and, on Windows, in the
  identifier the device declares.
- The engine reports which notes sound, and the window shows them; the
  terminal monitor still shows only how many. The window shows where an
  axis stands, not the controller value sent for it.
- The window lists a control of a game controller once it was used.
  Listing all of them from the start needs the input backends to say
  which controls a device has.
- The icons of `hidi tray` are drawn placeholders, and the window has no
  icon of its own; the maintainer will supply the art.
- `-connect` applies to every instrument. Choosing ports for each
  instrument separately needs those settings, and belongs with them.
