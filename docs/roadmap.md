# HIDI 2.0 roadmap

This file records what was agreed for HIDI 2.0, what is done, and what
remains, in the order it is planned. Update it in the commit that
completes or changes an item.

## How the work is organised

- `main` holds the released 1.x code and is not touched until 2.0 is ready.
- `dev` is the integration branch. A rule of the repository lets changes
  reach it through pull requests only, so every change comes as one: a
  branch `v2/<topic>`, a pull request into `dev` labelled `v2.0`, merged
  by the maintainer with a merge commit, never squashed.
- A commit is written as a pull request would be: it is a whole area that
  can be tried in one go, or one topic of fixes, and its message has a
  title and a description that says everything the commit contains and
  what could not be tried. A pull request gathers such commits and says
  what they are together. Before anything is pushed, the checks that CI
  runs pass.
- Pictures of an interface that changed go to the maintainer with the
  change.
- Nothing is written on GitHub beyond the pull requests themselves, and
  to nobody but the maintainer.
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

## Where things stand (26 November 2026)

`dev` holds everything up to the review of the whole code (pull requests
50 and 51). One pull request is open on top of it:

- `v2/round5`: the fifth round and what was asked for after it; see
  "Fifth round" and "Sixth round" below. In short: lights that keep up,
  keys left to the system, a list of the devices that play, the compact
  window with its curves, the profile editor with undo and explicit
  saving, learning mode, the Steam Controller, and then factory and user
  profiles, settings that are saved on purpose, HIDI's icon, the check for
  updates, and a Windows program without a console that carries its icon.
  Since then, the rounds below: the ninth, with new defaults, settings
  that the running HIDI holds, and one factory profile for every game
  controller; and the tenth, with the profile page finished for axes, the
  toggle as a command of the profile, and HIDI's own notes out of the
  settings.

Apart from HIDI itself: a change to raylib's Win32 backend is on the
maintainer's forks (`gethiox/raylib` and `gethiox/raylib-go`, branch
`win32-modal-loop-frames`), and `v2/round5-raylib-resize` is the fifth
round built with it and raylib-go 0.63.0. Tried by the maintainer on
Windows 11, it does what it is for: the window keeps drawing while it is
moved or resized, follows the mouse whatever the frame rate, and no longer
stops for half a second when the title bar is held. One pause is left and
stays: with a large drag distance set in Windows, a slow move of the held
title bar stops the window for up to half a second where it begins to
move.

**The release is built without that change** (the maintainer's decision):
2.0 uses raylib-go 0.62.2 as it is, and the window stands still while it
is moved or resized on Windows. The branch is kept for when the change has
found its way into raylib, or raylib-go 0.63 is taken up; it does not have
the sixth round.

### Thirteenth round (26 November 2026): what was reported, and what was done

| Reported or asked for | Done |
|---|---|
| "a profile is no longer tried" should be a debug message | It is. |
| Caps Lock held with "pass the toggle through" while HIDI is off stays lit, on the keyboard and in the window, until another key is pressed | The lights were shown before each event was recorded, and the toggle key was not shown again after it. It goes through the same step as every other key now. |
| Earlier, Later and Remove on the profile page are broken; rename to Move left and Move right | The buttons after a clicked one were drawn on top of it in that frame and took the same click: Earlier also moved back, removed, and opened the field for a new layout. Each button keeps its place now. On a factory profile the four buttons are faint. |
| Commands are the same in every layout; make them each layout's own, in the format and in the engine, without side effects on a release in another layout | Profile schema 2: every layout holds all of its bindings, `[global]` holds only `hidi_toggle`. Schema 1 files are read as before, and the window writes them in the new form with their first change. A new layout begins with the commands of the one it is made from. The engine already sent a release to what was pressed; it is held down by a test now. |
| Factory keyboard: no notes on Shift, Tab and Grave in Piano and Chromatic; the modifiers, space, arrows and the keys above them left to the system; velocity on F7 and F8 | Done. |
| Learning key yellow, velocity keys cyan by the velocity, chord key purple, dim when off | Three colours of their own among the settings: `learn`, `chord`, `velocity`. |
| A setting for the velocity step, 16, with values that do not drift at the ends | `input.velocity_step`, 16. The velocity moves along a ladder from the profile's velocity, stops at 1 and 127, and comes back the same way. |
| Holding + and − should repeat, 500 ms then every 100 ms | Every stepper and the note and controller steps on the profile page. |
| Buttons show no click | Every button is tinted green while held; a repeating one blinks with every click, half on, half off. |
| Keys that do nothing slightly red, as the lights show them | A dark red hint on the profile page and the main page. |
| A review of the code | See "Review of the code, third pass". |

### Review of the code, third pass (26 November 2026)

Fixed, each with a test that fails without the fix:

- Two devices toggling HIDI, or learning mode, at the same moment could
  count as one toggle.
- A profile of schema 1 whose layout bound a control of `[global]` in
  another spelling failed to load after the change to schema 2.
- A layout whose name has an escape in it could not be changed.
- `enabled = false` was not heeded for profiles in a directory named
  with `--profiles`.
- One half of a stick that stopped bending the pitch took the bend of the
  other half back.
- A MIDI device that could not be created at first was created twice when
  another profile came before the next try; the first stayed open, and on
  Linux a program connected to it heard nothing.
- Every change of a setting, the hysteresis from the window for one, let
  go of a keyboard that hid its toggle and took it again.
- A MIDI device that came back after HIDI was turned off meanwhile took
  its keyboard for a moment and sent where its axes stood.
- A profile tried from the window and one loaded from the directory could
  reach the manager in the wrong order.
- `hidi toggle` and the window's learn request read the state and set the
  other, which a toggle from a device at the same moment could undo.
- A request to the running HIDI could outlast the time the window gave it.
- On the profile page, the last chip of a hat could run into the next
  column, under the name and bar drawn there, and one click then chose or
  cleared both.
- What was typed in the settings went into the field for a new layout of
  the profile page under them, and Enter added the layout.
- A reversed axis split into its halves and joined again came back the
  other way round: the stick of the factory profile bent the pitch
  upside down.
- The warning that the file was changed elsewhere stayed after the change
  was taken back there.

Less work per event: an instrument no longer applies the manager's
requests before every event, only when there is one.

Made one: what a controller or pitch-bend action drives (three copies in
the engine), the type of a device, the key of a generated binding, the
line ending of a new line, the fingerprint of the profile directories,
the setters of the manager, the emptying of a parked device's queue.

Found in the window and left as they are, for the maintainer to weigh:
the keyboard is drawn by two functions, one for the main page and one for
the profile page, with two ways of colouring a key by what it does; the
main page names an axis "6 · X" and the profile page "6 · ABS_X"; the
colours of the lights are changed straight in the settings being edited,
the rest of the sheet through a copy; a click that asks HIDI for
something while another request is on its way does nothing.

Waits for the maintainer: "Pass the toggle key through to the system when
held" and "The toggle key is HIDI's alone" say the opposite of a toggle
that is held, and both are on as HIDI comes. HIDI does what the first
says, and what the table of the configuration reference says: tapped, the
key is hidden; held, it does what it normally does. The second then only
makes the key hidden on Linux as well, without `always_grab`. Which of the
two is meant decides whether the second's description or its effect is
to change.

### Twelfth round (26 November 2026): what was reported, and what was done

| Reported or asked for | Done |
|---|---|
| A customised profile needed a restart to be used by devices that were playing | "Customise Default" makes a copy that reads exactly like the factory profile, and a device was only given another profile when the content differed. Two profiles are the same now if they are the same file, kind and content. |
| "(generated)" is not seen; it belongs behind the name, and not on customised profiles | "Default gamepad (generated)" on the card and at the head of the page, for a factory profile with a rule. The marks on the groups are gone. |
| 127 buttons, the mapping stops at button 91 | As meant: from C1 the notes end at G8, note 127, which is button 91. The buttons after it are there to be bound and play nothing by the rule. |
| A button that detects axes which rest in the middle, with a range | "Detect axes that rest in the middle", within 10 % of the middle unless changed, on profiles that can be changed. Hats are not asked. |
| Warn about two controls on one controller | Ringed in yellow, with a line of explanation; saving and using are not refused. |
| The pitch bend's fill trembles | The clip it is drawn through rounded its corner and its size apart. Each edge is rounded by itself now. |
| The line in the middle of the input bars | Replaced by a blue triangle above the bar at where the axis rests as the profile reads it, on the profile page and the main page. |
| The page shows activation without the hysteresis | The page asks an engine of its own, the same code HIDI plays with, what is held and sent. |
| Dead zones in percent | Done. |
| Customised factory profiles distinct in the settings | "(factory override)" in yellow, and a yellow switch. |

### Eleventh round (26 November 2026): what was reported, and what was done

| Reported or asked for | Done |
|---|---|
| "Edit profile" shows some basic mapping, and nothing moves on it | The page took the last device it found with the profile, not the one whose card was clicked. It is opened for an instrument now and shows that one. |
| Show which device is being customised | "for <device>" beside the name of the profile. |
| Hint the device in "Customise for specific device" | The device the page was opened for is tinted green in the list. |
| Say that a mapping is generated | "(generated)" and "(partly generated)" on the groups of controls. |
| No channel down, channel up and panic in the default gamepad profile | Gone. The profile is the rule and the pitch bend on ABS_Y. |
| Controllers kept over off and on; two settings | `reset_pitch_bend`, on, and `reset_controllers`, off, in `[output]`, as switches in "State control". |
| Generating is slow, a second after a click | Nothing was generated slowly: HIDI looked at its profile directory once a second and waited for two looks to agree. The window now says when it wrote a profile and HIDI looks at once. The page also stays with its device while the device changes profile. |
| Dead zone 0 everywhere; one for the profile, overridden per axis | `deadzone` in `[defaults]`, 0 unless set; "own dead zone" on an axis. The factory pitch bend has none. |
| Hysteresis for axes that act as buttons, 2 % by default | `input.hysteresis`: half of it past the trigger point to press, half behind it to let go, kept within the travel. |
| The axis bar chooses the first mapping, also for hats and split axes; a chosen mapping marks its bar | Done. |
| The rp2350 gamepad shows as "gamepad", Windows shows "Extender Ultimate" | Not HIDI's to fix, as far as can be told from here: HIDI shows the name the device gives itself now, which the firmware has as "gamepad" unless set, for the device and for its HID interface alike. The name in Windows' list of game controllers is one Windows keeps in the registry for a vendor and product from when it first saw them. Setting "Device name" in the gamepad's configurator makes both agree. |

Also: an axis a device lists and has said nothing of is shown without a
bar and with a dash, in place of standing at its lower end.

### Tenth round (25 November 2026): what was reported, and what was done

| Reported or asked for | Done |
|---|---|
| The Gladiator's second set of buttons, brought up by a modifier, does nothing | Ours. Buttons past the 56th had no code: the table stopped at the last code Linux names. They now run on through all of Linux's codes and past them (`KEY_0x300` and up), so a stick with 128 buttons has them all. Linux itself stops at 80 for a joystick. **Not tried on Windows.** |
| `.devices.json` has to go | Gone. The settings offer the devices seen since HIDI started. |
| The "note" in `.written.json` has to go | Gone. |
| No state in the settings; state belongs in the local application data | A state directory, `%LocalAppData%\HIDI` and `$XDG_STATE_HOME/HIDI`: the record of written files, and `window.json` for what the window shows of which instrument. `show_input` and `show_output` are no settings any more. A directory named with `--config` keeps its state in `.state` inside itself. |
| "On and off" is "State control"; the switch for tapping renamed; the window's settings second | Done as asked. |
| The output view shows the controllers and the pitch bend | A bar each, labelled "cc16" and "PB", with the value last sent (0 to 127; -100% to +100%), in the order of the axes that drive them. |
| The profile page shows what the device does | Keys and buttons that are down are marked; axes have a bar; mappings fill by the value they would send or light up past their trigger point. Keys beyond the drawn keyboard appear once pressed. |
| Numbers always, names added | On the profile page and the main page. |
| "=>" between a control and what it does | Done. |
| No octave and layout on the hat | The hat's directions are buttons in the pool the rule fills. Pitch bend on Y stays. |
| "Customise Default"; one button for the devices | "Customise for specific device" unfolds them. |
| Buttons, axes and hats apart | Three groups. |
| What does "rests in the middle" do | It says the axis is a stick: a dead zone is then around the middle, the pitch bends both ways from there, and a controller counts 64 as its rest, to which it returns when channel or layout change. Without a dead zone a controller sends the same values either way. The page says so beside the switch. |
| Split into two controllers | "Split into two halves": one controller for each direction from the middle, 0 at rest. Also splits the pitch bend into up and down. "Join the halves" goes back. |
| Hats configurable like buttons | Each direction is chosen by its chip and changed like a button. |
| An axis as a button at a threshold, or as two | `threshold` on a binding, "triggers at" on the page; halves as above. |
| `hidi_toggle` as a command of the profile | Built: one binding in `[global]`, the setting as an override for all devices, tapping and "HIDI's alone" left as settings, and a keyboard without a toggle not taken from the system, said in the log and on its card. |

### Ninth round (24 November 2026): what was reported, and what was done

| Reported or asked for | Done |
|---|---|
| OpenRGB turned on and not saved worked for a while and stopped. Saving must do nothing but write the file | The window writes the settings file for its own sake too (which instruments show their input), and HIDI took the whole file over each time, undoing what was being tried. The running HIDI now holds the settings in use and tells them on request; the file is taken over only in what changed in it; saving writes and nothing else; closing the window takes nothing back. |
| Colours did not follow their sliders | They do, on the keyboard and in the window, while a slider is dragged. The MIDI ports to send to are followed at once as well (the router can be given other ports and lets go of the old ones). |
| Stutter at a limit of 30 frames a second, uneven motion at 15 | A limit that divides the monitor's rate is paced between two of the monitor's pictures, so frames land evenly; the curves move by the age HIDI reports for the newest step and the time since it arrived. **Not seen on a real monitor.** |
| "Revert" in the profile editor; "not saved" only when the profile really differs | Revert puts the page back to the file and forgets the undo steps. Whether there is something to save is asked of the two profiles as parsed (`config.Same`), so a key dragged away and back leaves nothing to save. |
| New defaults | Caps Lock, tapped, HIDI's alone; HIDI starts off, with its window open; learning lets an axis through at half its range. A file that names a toggle of several keys and says no more is read as it was meant. |
| Notes that appear when the pointer rests on a widget, not used anywhere yet | `kit.hint`: any widget can be given a text, shown beside it after 0.6 s. No widget has one yet, as asked. |
| Settings of the other system greyed out | `always_grab` on Windows, for one: shown, greyed, not to be changed. |
| Removing a profile of one's own, after a question | "Delete" beside each of the user's profiles; an overlay asks first and everything behind it waits. |
| Why are there still profiles for the Steam Controller, the Saitek and the PS4 controller | They are gone from the factory profiles. See the next section. |
| A change for one device only, by vendor and product | "Only for" with the device's name, on the page of the profile it plays with. See the next section. |

Found on the way: an axis that a controller had only listed, and not yet
reported, was sent as standing at its lower end whenever HIDI was turned
on: a pitch bent all the way down, on Windows at every start. Fixed, with
the tests that showed it.

A HIDI that starts turned off needs a way to be started turned on without
a settings file, on a machine without a screen for one: `--on`. The
end-to-end test in the virtual machine, which had failed since the new
defaults because it expected a HIDI that plays at once, now tests what
HIDI does as it comes: off at the start, turned on from outside, and
turned on again by a tap on Caps Lock.

Asked, and answered in the conversation rather than built:

- "The toggle is one key, tapped" and "the toggle key is HIDI's alone"
  differ only when the key is held: a tap is hidden from the system either
  way (on Linux only with keyboards kept grabbed), and a key held longer
  than half a second reaches the system unless it is HIDI's alone.
- The colour "other" lights a key that sends something that is neither a
  note nor a command: a controller, a program change, a pitch bend.
- The toggle as a command in a profile (`hidi_toggle`), per device, with
  the setting as an override: a proposal, see "To be decided".

### One generated mapping for every game controller (24 November 2026)

Proposed in the eighth round, built in the ninth, after the maintainer
asked why the profiles for single products were still there.

- HIDI comes with two profiles: keyboards, and game controllers. The
  second names a handful of controls and has a rule, `[generate]`, for all
  the others: button n plays the n-th note from C1, axis n sends the n-th
  controller from 16, the directions of a hat are buttons after the last
  button. The numbers are those the window shows, from the device's own
  list of controls in the order of their codes.
- The rule is the lowest layer, applied in every layout to what neither
  the layout nor `[global]` names. `"nothing"` silences a control.
- Whether an axis is a hat's is asked of the device, not read off its
  code: the pads of a Steam Controller use the hat codes and are no hats.
- The four profiles for single products are examples now, in
  `docs/examples/profiles`.
- A button on a profile's page ("Only for <device>" then, "Customise for
  specific device" since the tenth round) makes a profile of the user's
  for that vendor and product, out of what the page shows.

Decided on the way, and the maintainer's to overturn:

- Notes from C1 (36) instead of note 0, and controllers from 16 instead of
  0 to 6. Both were what HIDI 1 did; note 0 is below what instruments
  play, and controllers 0 and 6 choose a bank and enter data.
- Pitch bend stays on the Y axis for every controller, and the first hat
  keeps octave and layout. On a throttle the Y axis is a lever, and the
  pitch stays bent until the device has a profile of its own.
- Binding by number in a profile (`button.3 = ...`) was not built: names
  are what profiles and the window write, and the numbers stand beside
  them.

### Eighth round (23 November 2026): what was reported, and what was done

| Reported or asked for | Done |
|---|---|
| The window was broken on Windows: drawn wrong, and the pointer did not hit what it showed | The size of the interface had been laid on with raylib's camera mode, which in this raylib throws the scaling of the display away. On a display set to anything but 100 % (the maintainer's is at 125 %) everything was drawn smaller than the window's own points, in which the pointer is still measured. The size is now a scaling laid over what `BeginDrawing` sets up. **Not seen on a scaled display**: GLFW did not scale under Xvfb whatever was tried, so this rests on reading raylib. |
| HIDI keeps the monitors on | HIDI asked Raw Input for joysticks and game controllers, in the background. Windows counts what it delivers that way as somebody using the machine, and the Gladiator, like many sticks, reports without pause; so the idle time never left zero, whether HIDI was on or off. Another project found and fixed the same (scyto/hotas-bridge, pull request 23). Controllers are now read from their HID devices directly, by a goroutine each, and found by looking through the list of devices once a second; Raw Input is asked for keyboards only. **Written without a Windows machine and never run**: that controllers still play, come and go, and that the screen now turns off, are all to be tried. |
| The OpenRGB sign simpler | A ring with a hole and a slice cut out below it, both clear; the mesh gradient as before. |
| Everything that can be clicked lights up under the pointer | Switches, boxes, choices, the colour cells and sliders, the address field, the header's switches, and the controls of the profile editor. |
| Carrying bindings in the editor | Drag a control onto another: it gets what the first does, and the first does nothing. With Ctrl both do the same. One step to take back. The right button empties a control, with Ctrl leaves a key to the system. `ProfileDoc.Carry`, `config.Get`. |
| The settings in a better order, in sections, with switches two abreast | Six sections under lines across the sheet: on and off, devices, MIDI, lights, window, profiles. Switches stand two abreast where the sheet is at least 620 points wide, else one below the other. |
| Settings in use at once | Every change on the sheet is handed to the running HIDI (`settings` on the control socket, `Host.TrySettings`), which takes it over as it does a changed file. Saving only writes. Revert, and closing the window, hand back what the file has. |

### Review of the code, second pass (23 November 2026)

Asked for: the code at the highest standard, hacks on the side of the
window replaced by something meant, less written twice, and a look for
faults. Done in stages, each committed by itself:

- **The window has a toolkit.** `kit` owns what were package variables
  (fonts, size, cursor, clipping, deafness) and frames a frame; the
  widgets are its methods, one of each kind where there were two or
  three; `flow` wraps rows; `latest` hands the newest value to something
  slow; the settings page is a `sheet` with a method to a section.
  Checked by comparing nineteen pictures before and after, pixel for
  pixel.
- **A device's run loop is a type.** `runner.run`, 584 lines of closures
  over thirty variables, became `session` with fields and methods, by a
  tool written for the purpose so that nothing was retyped.
- **The run command lost its two programs within the program**:
  `parseRunOptions` and `liveSettings`.
- **Fuzzing** for everything that reads what it did not write: profiles,
  the editing of profile text, settings, what an OpenRGB server sends,
  the reports of the Steam Controller; and the engine itself, played at
  random on the built-in profiles: no note is left sounding, and its
  count of sounding notes agrees with the messages. Nothing was found.
- staticcheck with every check turned on reports nothing.

- **Game controllers say what they have.** A device can list its
  controls (`input.Describer`): Linux from the kernel's bits, Windows
  from the HID button and value capabilities, XInput by its fixed set.
  The window lists all of them from the start and numbers them from 0,
  by code, so the numbers hold for every controller of a kind;
  `window.control_names` shows names instead. The Windows part is
  written blind.

Left as it is, on purpose: `winmidi.open` (222 lines of one WinRT call
after another, which reads best in a row), and `cmdRun`, still 290 lines
of setting things up in order.

Found on the way and fixed: the media keys of keyboards that have them
on an interface of their own were never read on Windows (the
maintainer's HyperX). That interface is now read like a game controller
and its keys given to the keyboard it belongs to. **Written blind**: to
be tried.

### Smaller things about profiles, none of them built

- Renaming a profile from the window.
- A profile for one device from an instrument's card on the main page.
- Saying on the card why this profile was chosen for this device, where
  two fit.

TOML stays: profiles are changed line by line with notes kept, by hand
and by the window, and nothing asked for so far needs more than it has.

### Waiting for the maintainer

To be tried on Windows, none of which has run there:

- `hidi.exe` of a release build by double click: no console window, the
  icon in the tray with its new look, its menu, the window opened from it,
  "Quit HIDI". The same file from `cmd` and PowerShell: `hidi version`,
  `hidi doctor`, `hidi --headless` with Ctrl+C, output sent to a file,
  `start /wait hidi doctor`, and `hidi --console --headless` from a
  shortcut. Under Wine the first four work; `start /wait` showed nothing
  there.
- The icon of `hidi.exe` in Explorer and on the taskbar.
- The hand cursor over buttons and switches of the window: it is now set
  once a frame and repeated after every look at the pointer, because
  raylib's Win32 code gives the arrow back whenever the mouse moves.
- "Show in folder" beside a profile: Explorer with the file selected.
- A grabbed keyboard with the new defaults: playing, Caps Lock tapped to
  turn HIDI off and on, that Caps Lock does nothing else while held, and
  a toggle of three keys set in the settings.
- A key bound to `"passthrough"`: does it type while the others play.
- The four hats of the Gladiator, and whether its buttons play.
- `taskkill /F /IM midisrv.exe` while HIDI runs: do the instruments come
  back, and does the DAW find them again.
- An Xbox controller.
- The Steam Controller of 2026 with HIDI's own reader: axes, buttons, and
  that it stops moving the mouse.
- The window at 120 Hz: is motion smooth now.
- Learning mode, and the middle click on the icon.
- Game controllers after the change in how they are read: the Gladiator's
  axes, buttons and hats, plugging in and out while HIDI runs, the Steam
  Controller, and whether the monitors now turn off with HIDI running.
- The media keys of the HyperX: do volume, mute and the track keys show
  as keys of the keyboard, play when bound, and stop changing the volume
  while HIDI is on and the keyboard grabbed.
- The size of the interface on a display with scaling of its own, and
  that the pointer and what it points at still agree.
- Typing the OpenRGB address in the settings while the keyboard is not
  HIDI's alone.
- Game controllers with the generated mapping: that every button of the
  Gladiator plays a note of its own from C1 up, every axis sends a
  controller from 16 up, its other hats play notes, and that the numbers
  in the window are the ones the notes go by. The same for the Steam
  Controller, which now lists its controls, and an Xbox controller.
- That nothing is sent for an axis when HIDI is turned on before the axis
  was touched: no pitch bent down at the start.
- "Customise for specific device" on the page of the game-controller
  profile: the
  file it makes, and that the device plays with it within a few seconds.
- Settings in use at once and not saved: OpenRGB on and off, a colour
  following its slider, and that saving changes nothing that is heard or
  seen.
- A frame limit of 30 and of 15: is motion even on a real monitor.
- The Gladiator's upper buttons with the modifier held: do they show, with
  numbers above 55, and play.
- The toggle from the profile: Caps Lock as before; a keyboard with a
  profile of one's own without `hidi_toggle` keeps typing and says so;
  "One toggle for every device" set and taken back in the settings.
- The profile page with real devices: marks and bars following the
  device, split and join, a hat direction changed, a threshold.
- That `%AppData%\HIDI` holds only the settings and the profiles after a
  start, and `%LocalAppData%\HIDI` the rest.

To be tried with a keyboard OpenRGB lights, which was not at hand: that a
released key goes dark at once, how fast the lights follow while playing,
the glow in the window beside the real keyboard, and turning the lights
off and on in the settings while HIDI runs.

To be done by the maintainer before a release:

- `packaging/windows/midi-api` needs the address and the checksum of
  Microsoft's package `Windows.Devices.Midi2.<version>.nupkg`. Until it
  has them the Windows archives are built without the DLL and the release
  script says so. Microsoft's releases cannot be reached from where this
  was written, so neither could be filled in, and the unpacking was tried
  with a made-up package of the layout Microsoft's package description
  gives.
- The text of a release: the draft the workflow generates is edited by
  hand before publishing; a fixed template is still to be written if
  wanted.

To be decided:

- Whether the raylib change goes to raylib, and when HIDI moves to
  raylib-go 0.63.
- Whether the rest of the profile editor belongs in 2.0.
- What the generated mapping gives; see "One generated mapping" above.

### Left to do

- The rest of configuring in the window: program changes, keys with
  modifiers (a toggle of several keys among them), the options of a note,
  renaming a layout or a profile, a new profile from nothing.
- A profile that is tried and not saved stays in use if the window ends
  without a word, by a crash for one, until its file changes or HIDI
  starts again. A window that is closed takes it back.
- Notes under the pointer: the means are there, the texts are not.
- The window does not remember its size and place.
- Two keyboards with lights are written one after the other, so each
  follows at half the pace it could.
- No start at login yet.
- The check for updates is the window's: a HIDI that runs as an icon only
  does not look.
- What the review found and left as it is; see below.
- What comes back from the tests above.
- Turning on immutable releases, the first tag (which is also the first
  run of the release workflow), and the pull request from `dev` into
  `main`.

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

### Review of the whole code (17 November 2026)

Five independent readings of the code, each of one area and without
hints, all findings checked before anything was changed. What was fixed
is in the five commits that begin with "Review of". The one that
matters most for trying HIDI on Windows: the fix for the silent grabbed
keyboard from the fourth round had not been connected to the hook, and
is now.

Found and left as it is, by choice:

- A device that is not an instrument still takes a MIDI device name, so
  that a second device of the same name gets "... 2" though it is the
  only instrument of that name.
- With collisions set to "off", a note held by two keys shows as
  sounding until the second key is let go of.
- Buttons a controller declares outside the HID page for buttons, some
  direction pads, are not read on Windows.
- Every send to Windows starts a goroutine for its time limit: two
  hand-overs between threads for each note.
- The profile editor at the least width of the window has its last
  stepper off the edge; with two devices on one profile only one's
  presses choose a control.
- With a frame limit of 30 or less a tap shorter than a frame can go
  unseen by the window.
- With the lights on, every event asks the lamp to show twice; the hub
  looks for a keyboard that OpenRGB does not have every three seconds
  without end; the router holds its lock while it lists and connects.
- An entry of `connect` is a part of a name, so a port whose name is the
  beginning of another's selects both.

### Fifth round (20 November 2026): what was asked for, and what was done

| Asked for | What was done |
|---|---|
| The lights of a keyboard lag, and a released key can stay lit for up to a second. | Two causes, both in how OpenRGB takes updates that come faster than the keyboard is written. Released versions lose an update that arrives during a write; current ones take it only after the write, so updates queue up. The hub now sends a change at once, waits for the server to take it before sending the next (which is then the newest scene), and repeats what it sent last four times within a second. |
| Instruments compact by default; input and output shown by a switch. | Two lines per instrument. "show: input, output" on every card, remembered per device in `settings.toml`. |
| The numbers for input and MIDI tinted while not zero. | Done. |
| Axes horizontal again, more of them in a row, values in the fixed-width font. | A row takes as many as fit: five in the window's first size. |
| A blacklist or whitelist of devices in the settings. | `[devices]` with `mode` and `list`, by name; the settings sheet lists the devices that are connected and, in grey, those met before (kept in `.devices.json`). A running HIDI follows. |
| Keys in the main view coloured as in the profile editor, and glowing as the keyboard's lights do. | The snapshot carries what every key does and what the hub last sent to the lights. The glow is a blurred dot stretched over the key, added to the colour of the cap. |
| What is pressed shown in grey while HIDI is off. | Keys, buttons and axes. |
| A key held while HIDI is off lights up on the keyboard. | The colour `pressed`. |
| Keys a layout leaves to the system. | `KEY_SPACE = "passthrough"`. The device is shared instead of grabbed: the hook lets the key through on Windows, and on Linux it is typed again through `/dev/uinput`. The profile editor has a button for it. |
| The colours of the lights can be chosen, the one of passed keys too. | `[lights.colours]`, twelve colours, with sliders on the settings sheet; a running HIDI follows. The sixteen channel colours stay as they are. |

Not tried: anything of this on Windows, and the lights on a real keyboard;
the hub was tried against servers made for the tests, one of them slow.

Later the same day, into the same pull request:

| Asked for | What was done |
|---|---|
| A middle click on the icon turns HIDI off and on. | Linux did; Windows does now. |
| A much darker window. | Near black, with the panels a step above it. |
| Behind the two lines of an instrument, how much came in and went out, input green and MIDI yellow; the numbers tinted alike, in the terminal too. | The rates are worked out ten times a second over the second that just ended, and the window keeps twelve seconds of them per instrument. The status lines of the terminal colour the two numbers the same way. |
| The profile editor saves when asked, takes changes back, and a running HIDI plays with what is not saved. | Changes are kept in the window. A disk saves, yellow while there is something to save; Undo and Redo as buttons and as Ctrl+Z and Ctrl+Y. What is shown and not saved is given to HIDI over the control socket (`preview`) and stands in for the file until the file changes. A page left with changes keeps them, and a yellow dot marks the way back. |
| Layouts can be added, removed, put in another order, and one chosen to start with. | In the text of the file, like every other change: `config.AddLayout`, `RemoveLayout`, `MoveLayout`, and `layout` under `[defaults]`. |
| The colours of what the keys do, at half strength in the main view. | Done. |
| The settings open over the profile editor too. | The sheet lies over whatever is shown. |
| "blacklist" and "whitelist" as the names of the two modes. | Done. |
| Scrolling as a movement. | The target moves at once and the position follows it, by the time that passed and not by the frame: nine tenths of the way in 80 ms at any frame rate. In the instruments, the settings and the profile editor. |
| What all instruments together take in and send, at the top. | Before the version. |

Not tried: Windows; the keys of the editor with a keyboard HIDI has
grabbed, which do not reach the window, as the page says.

And after that:

| Asked for | What was done |
|---|---|
| The curves without averaging, a tenth of a second at a time. | An instrument keeps the last 122 steps of a tenth of a second, each the events of that step alone, and sends them with its state. The two numbers stay counted over a second. |
| The curves run evenly although they gain a value only ten times a second. | They are drawn moved to the left by the part of a step that has passed, with one value out of view at each end, so that none appears or goes at once. |
| The window between the two darknesses. | Half way. |
| The Steam Controller. | On Linux the kernel's `hid-steam` already presents Valve's controllers as game controllers; what HIDI lacked was a profile that knows what their controls are, since the general one takes a trackpad for a direction pad, and the names of the paddles (`BTN_GRIPL` and the other three, from a newer kernel header). Both are there. On Windows it stays for after 2.0; see there. |

Not tried: a Steam Controller. The profile is written from the kernel's
driver, not from a device.

Learning mode, asked for after that: a switch at the top of the window,
an entry in the menu of the icon, which blinks green and yellow twice a
second while HIDI is on and learning, and the `learn` command, on F10 in
the keyboard profile. The command was a key to hold before and let an axis
through beyond half its travel; it now turns a mode on and off that is one
for all instruments. In it every axis stands where it stood when the mode
began until it has moved by more than `learn_threshold` hundredths of its
range, 20 unless set; back within that it stands there again; and when the
mode ends, every axis says where it really is.

The maintainer then tried the controller of 2026 on Windows, through its
wireless receiver: HIDI showed its keyboard, and the controller went on
moving the mouse. Windows has nothing like `hid-steam`, so HIDI now reads
that controller itself there (`rawinput/steam.go`): it asks Raw Input for
the interface the controller keeps for itself, sends the two commands that
end its being a mouse and keyboard, as the kernel does, and turns its
reports into the same buttons and axes the kernel makes of them. The
keyboard it pretends to be is no longer taken for an instrument. What turns
reports into events is tested; what talks to Windows and to the controller
has never run. The controller of 2015 and the Steam Deck use an older form
of the reports and are not read on Windows.

### Sixth round (22 November 2026): what was asked for, and what was done

| Asked for | Done |
|---|---|
| OpenRGB turned on and off without starting HIDI again | The hub of the lights always exists and is switched; a changed address makes it connect anew. Endpoints are always opened both ways, since lights may be wanted later. |
| Profiles in two kinds, factory and user | `profiles/factory` is kept exactly as HIDI has it, read-only; `profiles/user` is the user's and comes first. `enabled = false` switches a user profile off. The earlier flat directory is converted on the first start. Logs, `hidi profiles` and the window say the kind. |
| The settings are not saved by themselves | The sheet changes a copy; the disk in its corner (raygui's floppy, doubled) saves, yellow while there is something to save; Revert drops; Ctrl+S saves. A dot on the gear says that changes wait. |
| Profiles on the sheet | Two groups. Yours: a switch, Edit, Show in folder. Factory: View, and Customise, which copies the profile to the user's directory and opens the copy. The editor shows a factory profile and refuses changes to it. Descriptions are cut at their end. "Open file" is gone. |
| Devices no longer there at the end of the list | Connected first, then seen before, then only named. |
| "Where things are" | HIDI's directory only. |
| The toggle key kept from the system (Caps Lock) | `input.toggle_exclusive`; a switch on the sheet. |
| The hand cursor showed for one frame | The cursor is set once a frame. On Windows raylib gives the arrow back on every mouse move, so there the wish is repeated after every look at the pointer. Not seen on Windows yet. |
| The icon | `internal/assets/icon` fills the shapes of the maintainer's drawing at any size. On: the H of the two outer keys and the bar green, the rest of the middle key a lighter green. Learning: yellow, alternating with green every half second. Off: grey without the letter. In the tray, in the header of the window, and in `hidi.exe` through go-winres at build time. |
| A nicer gear | A disc, teeth with sloping sides (eight at first, six since the seventh round), cut to a circle, a hole in the middle; drawn once as a picture and turned. |
| A check for updates | Opt-in (`window.check_updates`). The window asks GitHub's API for the newest release, once when it opens and once a day; the lower right corner says "up to date" in grey or "update available" in yellow, and a click opens the release. |
| The Microsoft DLL in the zip | The release script takes it from Microsoft's package by address and checksum and puts it, the `.pri` and Microsoft's licence into the Windows archives. Address and checksum are still to be filled in. |
| No console window | A release is linked with `-H windowsgui`; started from a command line HIDI joins that console; `--console` opens one. |

Not done on purpose: the raylib change stays out of the release.

### Seventh round (23 November 2026): what was asked for, and what was done

| Asked for | Done |
|---|---|
| The icon of the program neutral | All white keys white; the green ones are for the tray and the header, which show a state. |
| The OpenRGB logo as a vector drawing with a mesh gradient | `internal/assets/icon/openrgb.svg`: the wheel is one row of six patches around the middle, a corner each for red, yellow, green, cyan, blue and magenta; keyhole and base are plain shapes. Written by hand from measurements of the picture; **not opened in Inkscape**, which was not at hand, so the mesh is unverified. The window does not read the file: `icon.OpenRGB` draws the same shapes. |
| The logo beside "show" on every keyboard | Colour where HIDI drives the lights, grey where not. A click on a grey one asks HIDI to show what the lights would show (`simulate` on the control socket, `Manager.SetSimulated`): the sign turns yellow and the drawn keyboard glows as a lit one does. It also brings the drawn keyboard into view. Lasts while HIDI runs. |
| A gear of six teeth | Six. |
| Size of the interface | `window.scale`, 85 to 200 % on the sheet. Everything is laid out as before and drawn through a lens; the fonts are made anew for the size, so text stays sharp. Where the header or a card is too narrow, the least needed is left out and names are cut. The window keeps its size. |
| The OpenRGB address editable | A field on the sheet, empty by default with the usual address shown faintly, editable while the lights are off. An address needs a host and a port from 1 to 65535. |
| No height of the log | The setting is gone; a file that has it is still read. The window opens with the log folded. |
| Hover only while the pointer is over the window | `hovered` asks raylib first. |
| No grip on the status bar; the bar always the same | The bar always shows the last line and the word on updates. Unfolded, the row of levels is below it, then the log. |
| State changes logged as debug | Octave, transposition, channel, velocity, layout, chord, learning mode, and HIDI on and off. |
| No "the devices play" | Gone; what follows "on"/"off" stands at a fixed place. |
| "show" for one entry only | Remembered by name, kind and location. Lists of earlier builds, by name only, no longer match and are left alone. |


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
- The icon is the maintainer's drawing, `internal/assets/icon/hidi.svg`.
  HIDI does not read that file: its paths are copied into `icon.go`, so
  a changed drawing has to be copied there again.
- `--connect` applies to every instrument. Choosing ports for each
  instrument separately needs those settings, and belongs with them.
- The udev rule tags every input device with `uaccess`, so the person at
  the machine can read all of them. Narrowing it to keyboards and game
  controllers is possible, but would leave out the unusual devices a
  profile may name; it is left wide.
- `Windows.Devices.Midi2.dll` is shipped in the Windows archives, as the
  maintainer decided; the package is under the MIT licence, and its text
  goes along.
