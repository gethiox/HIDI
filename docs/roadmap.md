# HIDI 2.0 roadmap

This file records what was agreed for HIDI 2.0, what is done, and what
remains, in the order it is planned. Update it in the commit that
completes or changes an item.

## How the work is organised

- `main` holds the released 1.x code and is not touched until 2.0 is ready.
- `dev` is the integration branch. Since 17 November 2026 work is
  committed to it directly: no feature branches and no pull requests any
  more. Only one pull request remains, the final one from `dev` into
  `main`.
- A commit on `dev` is treated as a pull request was before. It is a
  whole area that can be tried in one go, or one topic of fixes, and its
  message is written as the text of a pull request was: a title, and a
  description that says everything the commit contains and what could
  not be tried. Before it is pushed, the same checks pass that CI runs.
- Pictures of an interface that changed go to the maintainer with the
  commit.
- For as long as the rule of the repository that asks for pull requests
  covers `dev`, such commits wait on the branch `v2/docs`, in order, to
  be pushed to `dev` as they are once the rule allows it.
- Up to pull request 49 the work went through feature branches and pull
  requests with merge commits, each labelled `v2.0`.
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

Windows, second round. Tried on the maintainer's machine (Windows 11 build
26300, `Windows.Devices.Midi2.dll` from Microsoft's Preview 11 NuGet
package beside `hidi.exe`), with `dev` at 24c54ff.

### What works there

- `hidi run -v --no-grab` after a fresh start of Windows: seven input
  devices became instruments within a second (a HyperX keyboard, a Razer
  mouse that presents a keyboard, two VKB joysticks, a Saitek throttle,
  vJoy, a device called "gamepad"). Their MIDI devices were created one
  after another, about 100 ms each.
- **Keyboard input works**: keys of the HyperX played notes (NoteOn and
  NoteOff logged), so Raw Input delivers them. The toggle combination
  turned HIDI off and on.
- Game controllers through Raw Input send controller values.
- `hidi doctor` creates three devices one after another; Bitwig shows
  incoming MIDI.
- Leaving that first `run` with Ctrl+C removed all six MIDI devices at
  once, without a hang.
- The duplicates are gone for the keyboard and the mouse.

### What was done about the rest, and waits to be tried there

1. **Removing a MIDI device sometimes never returns**, and then the MIDI
   service answers nobody. This is a fault of the service in Windows,
   not of HIDI: microsoft/MIDI issues 1047 and 1236 describe it exactly
   (teardown of a virtual device hangs in `MidiSession::Close` or
   `DisconnectEndpointConnection`, and every later client hangs when it
   creates a session or device). Microsoft announced the repair for the
   Windows update at the end of November 2026; a related deadlock, when a
   virtual device is opened while another shuts down, was repaired in
   their source on 27 October 2026. Waiting half a second before a
   removal was tried (pull request 47) and made no difference, so HIDI
   does not wait any more. What helps without restarting Windows is
   ending the service's process, `taskkill /F /IM midisrv.exe` as
   administrator; `Restart-Service` hangs. HIDI says so when it happens.
   To try again once the maintainer's Windows has the repair.
2. **The second device of a model could not be created**: status 808 for
   the name "HIDI VKBsim Gladiator EVO R #2" with the identifier
   "HIDI-HIDI-VKBsim-Gladiator-EVO-R--2". The devices of `hidi doctor`,
   "HIDI test device 2" and "3", were created without trouble, so the
   number is now written the same way, without "#", and the identifier
   has "HIDI" once and never two dashes in a row. Why that one joystick
   appears as two devices is a separate question: `hidi devices` now
   prints where each device is found, which shows whether Windows gives
   them one container or two.
3. **An axis that does not rest still flooded**: done, see `axis_threshold`
   in the profile reference.
4. **Logging**: done. Single MIDI messages are logged only with
   `--log-midi`; `hidi run` keeps lines about its state at the bottom of
   the terminal; every interface shows input events and MIDI messages a
   second.

5. **One key to turn HIDI off and on** (asked for after the third
   round): `toggle_tap` makes a toggle of one key work by a tap, with a
   longer hold passed on as the key itself. Windows hides the tap from
   the system through its keyboard hook. Linux does only with
   `always_grab`, which keeps keyboards grabbed while HIDI is off and
   types their keys again through uinput; the maintainer chose to leave
   that off by default, and the default toggle stays the three keys.
   Untried on Windows and on a real Linux desktop. The udev rule gives
   access to `/dev/uinput` for it.
6. The buttons of a joystick play notes in the default profile, and two
   instruments of one name in one place keep their order.

Still open there:

- A mouse that presents a keyboard interface (Razer Basilisk V3) is an
  instrument. Still no sure way to tell.
- `hidi run` with grabbing (without `--no-grab`), an Xbox controller
  through XInput, and what a HIDI that was killed leaves behind, are
  still untried.
- The lines at the bottom of the terminal work in the Windows terminal.
- The one-key toggle (`toggle_tap`) is untried on Windows.

### Fourth round (17 November 2026): what was reported, and what was done

Reported from the maintainer's machine, with `hidi run` and `hidi gui`:

| Reported | Done |
|---|---|
| With the keyboard grabbed, neither the combination nor a tapped key turned HIDI off, and the window showed no key until HIDI was turned off by its switch. | Windows does not report a key through Raw Input once the hook swallowed it. The hook now hands such keys to HIDI. It does not know the keyboard, so they go to the keyboard that typed last while keys passed. |
| Of the four hats of a VKB Gladiator only the first worked, and it moved all four. | Values are read by the index Windows gives each (`HidP_GetData`), not by usage, which four hats share. |
| HIDI did not recover when the MIDI service was ended with `taskkill`. | An instrument whose MIDI device breaks gets a new one. HIDI watches the service's process and declares its devices lost when it ends. |
| At 120 Hz with no frame limit the window looked like 60. | The window was drawn fast enough, but got HIDI's state only twenty times a second. It now asks for it every 4 ms. |
| No setting for a frame limit while the window is unfocused. | There was one, under a name that did not say so. Now two: "Frame limit" and "Frame limit while the window is unfocused". |
| Scrolling past the end of the settings flickered for a frame. | The page is held to its length before it is drawn. |
| The settings should be a sheet over the main view, a little transparent. | Done; what lies under it takes no input, a click beside it closes it. |
| The gear has too many teeth too close together, and turns too slowly. | Six teeth instead of eight, once around in eight seconds. More precise wishes are to come. |
| Axes should stand upright, or need less width. | Upright bars with name and value below. |

Also done, as asked for the same day:

- **`hidi` alone starts the application**: instruments and icon in one
  process, the window as a second one started from the icon, by
  `--window`, or by the setting `window.show_at_start`. `--headless`, or
  `hidi run`, runs the instruments only.
- **Options with two dashes**, through `github.com/spf13/pflag`.
- **The README rewritten for users**, with the preview status of Windows
  and the way out of a hung MIDI service.

None of the Windows parts of this ran on Windows. To be tried there:

- a grabbed keyboard playing and toggling, with the combination and with
  `--toggle=KEY_CAPSLOCK --toggle-tap`;
- the four hats, and whether the buttons of the Gladiator play;
- `taskkill /F /IM midisrv.exe` while HIDI runs: do the instruments come
  back, and does the DAW find them;
- `hidi.exe` by double click: the icon, its menu, the window from it,
  "Quit HIDI", and what is seen of the console window;
- an Xbox controller.

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
  and `hidi run --connect`.
- The control interface: a local socket to the running HIDI, with
  `hidi status`, `hidi on`, `hidi off`, `hidi toggle` and `hidi monitor`.
- Axis settings in a profile: the rate at which an axis is passed on,
  how far a resting axis has to move to count as moved, and what its
  controller does on a channel or layout change.
- `hidi run` on a terminal: the state of HIDI and its instruments in the
  last lines, the log above; input events and MIDI messages a second for
  every instrument, in every interface.
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

In planned order. Each item is roughly one commit on `dev`, as it was one
pull request before.

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
    - Done: the toggle combination, by pressing the keys, and the MIDI
      ports to connect to, from the list of those that exist. Still open:
      a running HIDI follows changed ports only when it starts again,
      since the router cannot drop connections yet; and the lights are
      not on the page.
    - Done in a profile: an axis sends a controller or bends the pitch,
      with centred, reversed and dead zone; a key sends a controller.
      Still open there: the two halves of an axis bound apart, program
      changes, keys with modifiers, and the options of a note; adding
      and removing layouts; making a new profile and saying which
      devices it is for.

### Features

### Release

10. Done except packages for distributions: release archives, a udev
    rule so that HIDI needs neither root nor group changes, `hidi doctor`
    saying whether input devices can be read, and the CI workflow's
    actions updated and pinned to commits. A `.deb`, an AUR package or
    the like can follow when someone asks.
11. Done, untried until the first tag: `scripts/release.sh` builds
    reproducibly (`-trimpath`, the toolchain of `go.mod`, no build time
    or build ID, archives with the commit's time in a fixed order); the
    release workflow runs it twice and compares, attests build
    provenance and makes a draft release. Left to the maintainer: turn
    on immutable releases in the repository settings, and decide how
    `Windows.Devices.Midi2.dll` reaches Windows users (it is not in the
    archive).
12. Done: the README is written for users. The configuration reference
    and the description of the architecture were kept up with each
    change and are as they were otherwise.
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
- That `hidi run --connect` reaches a real USB MIDI interface, and
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

- HIDI is not started at login. A switch for that, an entry among the
  desktop's autostart programs on Linux and in the registry on Windows,
  can follow.
- On Windows `hidi.exe` is a console program, so that its commands write
  to a terminal. Started by a double click it leaves its console at once,
  which may still show for a moment. If that disturbs, the alternative is
  a second program file without a console for the application.
- The window cannot be brought to the front from the icon when it is open
  already but covered.
- With two keyboards grabbed on Windows, both play the instrument of the
  one that typed last before HIDI was turned on. Telling them apart while
  grabbed needs a driver.
- `spf13/pflag` is held at 1.0.10, because 1.1.0 writes "[=true|false]"
  after every switch in the help.

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
- `--connect` applies to every instrument. Choosing ports for each
  instrument separately needs those settings, and belongs with them.
- The udev rule tags every input device with `uaccess`, so the person at
  the machine can read all of them. Narrowing it to keyboards and game
  controllers is possible, but would leave out the unusual devices a
  profile may name; it is left wide.
- A release has no file for `Windows.Devices.Midi2.dll`. Whether HIDI may
  ship Microsoft's file, or has to send users to Microsoft's package, is
  to be decided before a Windows preview is offered.
