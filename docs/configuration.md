# Configuration

HIDI keeps everything you can change in one directory:

| System | Directory |
|---|---|
| Linux | `~/.config/HIDI` (or under `$XDG_CONFIG_HOME`) |
| Windows | `%AppData%\HIDI` |

```
HIDI/
  settings.toml    how HIDI itself behaves
  profiles/
    factory/       the profiles HIDI comes with, one file for each; read-only
    user/          your own profiles, which come before the factory ones
```

What HIDI notes for itself is not in this directory: which files it wrote
there, and how the window was left (which instruments show their input and
output). That is state, not something you set, and it stays on the machine:

| System | Directory |
|---|---|
| Linux | `~/.local/state/HIDI` (or under `$XDG_STATE_HOME`) |
| Windows | `%LocalAppData%\HIDI` |

A directory named with `--config` that is not the usual one keeps its state
in a directory `.state` inside itself. HIDI keeps no list of the devices it
has met: the settings offer the devices seen since it started.

`hidi config` prints these places. The `--config DIR` option of every command
names another directory, and `--config ""` uses no directory at all: HIDI then
runs with what it comes with.

On its first start HIDI creates the directory and fills it: the
settings with their defaults, and the profiles HIDI comes with in
`profiles/factory`. From then on the directory is what counts.

- `profiles/factory` is HIDI's: every start makes it exactly what this HIDI
  comes with, so a newer HIDI brings its improved profiles along. The files
  are marked read-only. What you change there does not last.
- `profiles/user` is yours: HIDI never changes or removes a file there by
  itself. A profile of yours is used before a factory profile that fits a
  device as well, so the way to change how a device plays is a copy of its
  factory profile in `profiles/user`. "Customise" in the window makes that
  copy, and "Customise for specific device" makes one for a single
  product.
- A directory from an earlier HIDI 2, with the profiles in `profiles`
  itself, is brought into this shape on the first start: files HIDI wrote
  and you did not change are dropped, since `factory` has them, and the
  others move to `profiles/user`. HIDI logs each one.
- A directory that HIDI did not make, with things in it that HIDI 2 does not
  keep there, is left untouched. HIDI says so and runs with what it comes
  with until you move the directory away or name another.

## Settings

`settings.toml` is read when HIDI starts. A setting that is left out keeps
its default, and an option given on the command line comes before the file.
A file with a mistake is not used: HIDI says what is wrong and runs with the
defaults.

The running HIDI holds the settings it goes by. They are the file's until
something else is handed to it:

- Everything here can also be changed in the window, on the sheet the gear
  opens. What is changed there is in use at once, without being written:
  the toggle, what is grabbed, which devices play, the MIDI ports, and the
  lights, down to a colour following its slider. The window says that it
  is not saved for as long as it differs from the file. Closing the window
  changes nothing; opened again, it shows what is in use.
- Saving on that sheet writes the file and does nothing else. Nothing is
  read back or applied again because of it.
- A file changed by hand is noticed within about two seconds, and what
  changed in it is taken over. A changed file with a mistake is reported
  and changes nothing.

`start_on` and `show_at_start` matter when HIDI starts, by their nature.

```toml
schema = 1

[input]
toggle = []
grab_keyboards = true
grab_gamepads = false
start_on = false
toggle_tap = true
learn_threshold = 50
hysteresis = 2
velocity_step = 16

[devices]
mode = "blacklist"
list = []

[output]
connect = []
reset_pitch_bend = true
reset_controllers = false

[lights]
openrgb = false
address = "127.0.0.1:6742"

[lights.colours]
white = "#005500"
black = "#000055"
c = "#555500"
unbound = "#440000"
playing = "#ffffff"
external = "#ffffff"
command = "#ff00ff"
panic = "#ff0000"
learn = "#ffd000"
velocity = "#00e0ff"
other = "#003030"
passthrough = "#404040"
idle = "#181818"
pressed = "#ffffff"

[window]
show_at_start = true
fps = 0
background_fps = 60
scale = 100
check_updates = false
control_names = false
```

| Setting | Meaning | Option |
|---|---|---|
| `input.toggle` | One toggle for every device: a key, or several to be pressed together, that turns HIDI off and on whatever the profiles say. Empty, as HIDI comes, leaves it to the profiles, each of which binds a key to `hidi_toggle`. | `--toggle` |
| `input.grab_keyboards` | Keep keyboards from typing while HIDI is on. A keyboard that has no toggle, by its profile or by the setting above, is left typing all the same: nothing on it could turn HIDI off again. HIDI says so in its log and on the instrument's card. | `--no-grab` |
| `input.grab_gamepads` | Also keep game controllers from other programs. | `--grab-gamepads` |
| `input.start_on` | Whether HIDI is on when it starts. | `--on`, `--off` |
| `input.toggle_tap` | Pass a toggle of one key through to the system when it is held: a tap toggles and is hidden, held longer the key does what it normally does. Off, the key toggles as it is pressed and is HIDI's alone. See below. | `--toggle-tap` |
| `input.learn_threshold` | How far an axis has to move to get through in learning mode, in hundredths of its range, 1 to 100. See the `learn` command. | |
| `devices.mode` | How `devices.list` is read: with `"blacklist"` every keyboard and game controller plays except the listed ones, with `"whitelist"` only the listed ones do. | |
| `devices.list` | Names of devices, as HIDI shows them. Two devices of one name are treated alike. | |
| `input.hysteresis` | The hysteresis of an axis that acts as a button, in hundredths of its travel, 0 to 100. An axis that plays a note or runs a command has to pass its trigger point by half of this to count as pressed, and to come back behind the point by half of this to count as let go, so that an axis resting near the point does not trigger over and over. Neither goes past the ends of the travel: with the point at 50 % and a hysteresis of 100, the axis is pressed at its far end and let go at rest. 0 turns it off. | |
| `input.velocity_step` | How far `velocity_up` and `velocity_down` move the velocity, 1 to 127. The velocity goes along the values this far apart from the velocity the profile starts with, and stops at 127 and 1; the step back from either end goes to the value before, so the velocity of the profile is always reached again. With 16, as HIDI comes, and a profile at 64: 80, 96, 112, 127 up, and 48, 32, 16, 1 down. | |
| `output.connect` | Existing MIDI ports every instrument also sends to. | `--connect` |
| `output.reset_pitch_bend` | Turning HIDI off puts the pitch bend back to no bend. `true` as HIDI comes. | |
| `output.reset_controllers` | Turning HIDI off puts every controller back to its rest. `false` as HIDI comes: a controller stays as it was last sent, and is not sent again on turning on while its axis is where it was. | |
| `lights.openrgb` | Light the keys of keyboards through an OpenRGB server. | `--openrgb` |
| `lights.address` | Where that server listens, as `"host:port"`. Empty, which is the default, means `127.0.0.1:6742`. | `--openrgb ADDRESS` |
| `lights.colours` | The colours of the keys, each as `"#rrggbb"`. See below. | |
| `window.show_at_start` | Open the window when HIDI starts; without it HIDI starts as an icon in the tray. | |
| `window.check_updates` | Let the window ask GitHub, once when it opens and once a day after, whether a newer HIDI has been released. The answer is shown in the lower right corner; a click opens the release. Nothing is installed, and the request says nothing but the version of HIDI that asks. Off unless turned on. | |
| `window.control_names` | The window numbers the buttons and axes of game controllers from 0, in an order that is the same whenever a controller of that kind is plugged in. With this the names profiles use, such as `BTN_SOUTH`, stand beside the numbers. | |
| `window.fps` | The most the window is drawn per second; 0 sets no limit, and the window keeps to the monitor's refresh rate. | `--fps` |
| `window.background_fps` | The same limit for while another window has the focus: 60 as HIDI comes; the window offers 10, 15, 30 and 60, and 0 in the file sets none of its own. | `--background-fps` |
| `window.scale` | The size of everything the window draws, in hundredths of its ordinary size, 50 to 300. | |

What a fresh HIDI does, then: Caps Lock, which the keyboard profile binds
to `hidi_toggle`, turns it off and on with a tap and does nothing else any
more, and HIDI starts turned off with its window open, so that the keyboard
types until it is asked to play.

`toggle_tap` is about a toggle of one key, wherever it comes from. A
toggle of several keys is pressed, and it does not apply.

A setting that only one system has, `grab_gamepads` for one, is shown on
the sheet of the other system too, greyed out and not to be changed there.

`always_grab` and `toggle_exclusive` were settings of earlier HIDI 2
builds, about how a toggle of one key is kept from the system, which
`toggle_tap` alone says now. A file that still has them is read without
complaint, and the lines are gone the next time the file is saved.

`window.log_height`, `window.show_input` and `window.show_output` were
settings of earlier HIDI 2 builds. A file that still has them is read
without complaint, and the lines are gone the next time the file is saved:
the window always opens with the log folded away, and which instruments
show their input and output is kept with HIDI's state.

#### The colours of the lights

| Colour | What it lights |
|---|---|
| `white`, `black`, `c` | Keys that play a white key of a piano, a black one, and a C. |
| `unbound` | Keys that do nothing. |
| `playing` | A key whose note sounds because of this instrument. |
| `external` | A key whose note another program sent on the instrument's channel. |
| `command` | The keys that run commands, magenta as HIDI comes. They show state in three steps of brightness, of which this colour is the brightest; the chord key is dim until a chord is remembered. |
| `panic` | The panic key. |
| `learn` | The key of learning mode: dim while the mode is off; while it is on, it blinks between dim and this colour, together with HIDI's icon in the tray and in the window. Yellow as HIDI comes. |
| `velocity` | The keys that move the velocity, both alike: dim at velocity 1, this colour at 127, and in proportion between. Cyan as HIDI comes. |
| `other` | Keys that send something else: a controller, a program change, a pitch bend. |
| `passthrough` | Keys that are left to the system. |
| `idle` | Every key while HIDI is off. |
| `pressed` | A key that is held while HIDI is off. |

The keys that change the MIDI channel take the colour of the channel, and a
note another program sends on another channel takes that channel's colour;
those sixteen colours are fixed.

#### One key that turns HIDI off and on

This is how HIDI comes: the keyboard profile says

```toml
[global]
KEY_CAPSLOCK = "hidi_toggle"
```

and the settings say

```toml
toggle = []
toggle_tap = true
```

Tap the key, pressed and let go within half a second, and HIDI turns off or
on. The key plays nothing, and the tap is hidden from the rest of the
system: Caps Lock does not switch capitals. Hold it longer and it does not
toggle; it is passed through to the system and does what it normally does.

With `toggle_tap = false` ("Pass the toggle key through to the system when
held" off in the window) the key turns HIDI off and on as it is pressed,
and is HIDI's alone: the system never sees it, tapped or held, with HIDI on
or off.

Another key is another line in a profile of your own, and several keys
pressed together are written as a combination:
`"KEY_LEFTCTRL+KEY_LEFTSHIFT+KEY_LEFTALT" = "hidi_toggle"`. A profile has one
such line, in `[global]`, since the toggle has to work in every layout and
while HIDI is off. `toggle = ["KEY_SCROLLLOCK"]` in the settings names one
toggle for every device instead, and the profiles' are then not looked at.
A combination is never kept from the system.

The key is kept from the system only where keyboards are grabbed
(`grab_keyboards`, as HIDI comes); otherwise the keyboard is the system's,
and so is the key. On Windows a key is hidden whichever keyboard it is
pressed on.

Linux cannot keep one key of a keyboard from the system: a keyboard is
grabbed whole or not at all. So to hide the toggle while HIDI is off, HIDI
keeps the keyboard grabbed then as well, and types every key but the
toggle again on a keyboard of its own, named after yours with "(through
HIDI)". Everything you type then passes through HIDI for as long as it
runs. If HIDI were ever to hang, the keyboard would do nothing until HIDI
is ended from elsewhere; when HIDI ends or is killed, the keyboard is yours
again at once. It needs write access to `/dev/uinput`, which the udev rule
that comes with HIDI gives; without it the keyboard is the system's while
HIDI is off, toggle key and all, and HIDI says so in its log. A part of a
keyboard that has a wheel or a pointer stays with the system and is not
held.

# Profile files

A profile describes one instrument: which devices it applies to, the state it
starts in, and what each control does. Profiles are [TOML](https://toml.io)
files.

HIDI comes with two profiles, one for keyboards and one for game
controllers of every kind, so a device works without any configuration.
They are the files in `profiles/factory`, and the best starting point for
your own. Neither is written for one product: what the profile for game
controllers does not name plays by a rule (see `[generate]` below), so a
pad of ten buttons and a stick of forty are both instruments from the
start.

Profiles written for single products, which earlier HIDI 2 builds came
with, are kept as examples in
[`docs/examples/profiles`](examples/profiles): a PS4 controller with its
touchpad, the two halves of a Saitek X-56, the Steam Controller. Copy one
into `profiles/user` to use it.

HIDI checks a profile completely when it loads it and reports every problem it
finds at once.

## Where your profiles live

HIDI reads every `.toml` file in `profiles/factory` and in `profiles/user`.
The first are HIDI's and kept as they come; the second are yours.

```
hidi profiles            list the profiles, with their kind, and check the files
```

To change what a device plays, copy its factory profile into `profiles/user`
and change the copy; "Customise" beside a factory profile in the window does
the copying and opens the copy. That copy is for every device of the kind,
as the factory profile is.

For one device to play differently from the others of its kind, it needs a
profile that names it. On the page of a profile in the window, "Customise
for specific device" unfolds the devices that are playing with that
profile. A click on one makes a profile of yours that is the one shown,
named after the device, for that product alone:

```toml
[match]
type = "gamepad"
vendor = 0x231d
product = 0x0200
```

The device plays with it from then on, and every other device stays with
what it had. Two devices of the same product share the profile.

A profile of yours can be switched off without removing it, with the switch
beside it in the window or by writing

```toml
enabled = false
```

at the top of the file. The device then plays by its factory profile again.
"Delete" beside it removes the file, after asking.

On the page of a profile, "not saved" means that what is shown differs from
the file: a change that is taken back by hand, a key dragged away and back
for one, leaves nothing to save. "Revert" puts the page back to what the
file says and forgets the steps that could be undone.

The `--profiles DIR` option reads profiles from another directory instead of
HIDI's own. Those are used beside the profiles inside HIDI, not in place of
them, and win over them. `hidi profiles --export --profiles DIR` writes the
profiles HIDI comes with into such a directory.

When several profiles select a device, the one that describes it most closely
wins: a profile for one product beats one for all keyboards. Between two that
are equally close, yours wins over a factory profile, and of two of the same
kind the file that sorts later by name.

### Changes apply while HIDI runs

A running HIDI notices within about two seconds when a file in the directory
is added, changed or removed, and loads the profiles again.

- An instrument whose profile did not change keeps playing undisturbed.
- An instrument whose profile changed releases its notes and continues with
  the new profile. Its MIDI device stays, so connections in other programs
  survive. It starts again from the profile's defaults.
- A device that had no profile becomes an instrument when one appears, and an
  instrument whose profile was removed stops.

A file with a mistake is not used, and HIDI logs what is wrong with it and
where. Until it is fixed, the device falls back to the next best profile,
if there is one. `hidi profiles` shows the same report and exits
with status 1, which makes it usable as a check.

## Example

```toml
schema = 2
name = "My keyboard"

[match]
type = "keyboard"

[defaults]
channel = 1
velocity = 100
layout = "Piano"

[global]
KEY_CAPSLOCK = "hidi_toggle"

[[layout]]
name = "Piano"

[layout.bindings]
KEY_F1 = "octave_down"
KEY_F2 = "octave_up"
"KEY_LEFTSHIFT+KEY_F1" = "transpose_down"
KEY_ESC = "panic"
KEY_Z = "C1"
KEY_S = "C#1"
KEY_X = "D1"
KEY_SPACE = { cc = 64 }
```

## Top level

| Setting | Meaning |
|---|---|
| `schema` | Required. The version of this format the file is written for. The current version is `2`. HIDI reads files written for older versions and upgrades them as it loads them; the window writes a file in the current version with the first change made to it there. |
| `name` | A name for the profile, shown in logs and the user interface. |
| `enabled` | `false` switches a profile of your own off: HIDI reads it and does not use it. Left out, the profile is used. A factory profile is always used. |

## `[match]`: which devices use the profile

Every setting is optional. A device must agree with all the settings that are
present. When several profiles fit a device, the one with the most settings
wins, so a profile for one specific model takes precedence over a general one.

| Setting | Meaning |
|---|---|
| `type` | `"keyboard"`, `"gamepad"` or `"mouse"`. |
| `bus`, `vendor`, `product`, `version` | The hardware identifiers the device reports, usually written in hexadecimal, such as `vendor = 0x054c`. |
| `serial` | The serial number of one particular device. Many devices, keyboards especially, do not report one. |

## `[defaults]`: the starting state

These are also the values the reset combinations return to.

| Setting | Default | Meaning |
|---|---|---|
| `octave` | `0` | Octave shift applied to every note, from -10 to 10. |
| `transpose` | `0` | Shift in semitones, from -127 to 127. |
| `channel` | `1` | MIDI channel, from 1 to 16. |
| `velocity` | `100` | Note velocity, from 1 to 127. |
| `layout` | the first layout | Name of the layout selected at start. |
| `collision` | `"interrupt"` | What happens when a note that is already sounding is played again, which is possible when two controls are bound to the same note. See below. |
| `axis_on_change` | `"keep"` | What happens to a controller when a change of channel or layout takes its axis away. See below. |
| `axis_rate` | `120` | How many positions a second an axis passes on, from 1 to 1000; `0` passes every one. |
| `axis_threshold` | `0.01` | How far a resting axis has to move before it counts as moved, as a fraction of its travel, from 0 to 0.5; `0` passes everything. See below. |
| `deadzone` | `0` | The dead zone of every axis in the profile that names none of its own, bound by a line or by the `[generate]` rule: the fraction of travel next to the rest position that is ignored, from 0 to below 1. |

Axes when the channel or layout changes: pitch bend, and a controller driven
by an axis marked `centered`, always return to rest, so that nothing stays
bent on the channel you left. For an axis that stays where you put it, such as
a throttle lever driving volume, you choose:

| `axis_on_change` | The controller on the old channel |
|---|---|
| `keep` | keeps its value, so a volume does not jump to zero |
| `rest` | returns to zero, so nothing is left behind |

`axis_rate` thins out the positions of sticks, triggers and levers, which can
report a thousand times a second. The last position always arrives. Keys are
never delayed.

`axis_threshold` keeps a lever that is left alone quiet. Few axes report one
steady position at rest: they waver between two neighbouring ones, and each
waver would be sent as a controller message, without end. So HIDI takes the
position an axis is at as its rest position and ignores what stays within
the threshold of it. Once the axis is further away than that it is moving,
and every position is passed on in full detail, small steps included. When
it has stayed in one place for a quarter of a second, that place is its new
rest position. A slow movement is not lost: it leaves the rest position
behind like a fast one. The two ends of the travel always arrive. Raise the
value for a worn lever that wanders more; `0` turns this off.

Collision modes:

| Mode | A second press of a sounding note | The note ends when |
|---|---|---|
| `interrupt` | ends the note and starts it again | the last control holding it is released |
| `no_repeat` | does nothing | the last control holding it is released |
| `retrigger` | starts it again without ending it first | the last control holding it is released |
| `off` | starts it again without ending it first | any control holding it is released |

## `[generate]`: the controls the profile does not name

A game controller can have a dozen controls or a hundred, under names that
differ from one to the next. A profile does not have to list them:

```toml
[generate]
notes = "C1"        # button n plays the n-th note from C1
controllers = 16    # axis n sends the n-th controller from 16
```

Either setting can be left out. With them, every button and axis of a
device that has no line gets a part by its number:

- The numbers are the ones the window shows controls by: the buttons of the
  device in the order of their codes, counted from 0, and the axes
  likewise. They depend on nothing but what the device has, so the same
  device is the same instrument every time.
- The directions of a hat are buttons, after the last button: two for each
  of its axes.
- Controllers that mean something of their own are stepped over: 0 and 6,
  32 to 69, 96 to 101, and 120 to 127. From 16 that is room for sixty axes.
- In each layout, a control is filled in if it has no line in that layout
  and is not the toggle in `[global]`. A line for one half of an axis, or for a
  combination that ends in the control, counts as a line.
- Only the main part of a device is filled in, not a touchpad or motion
  sensors, which would send all the time. Name those to use them.
- A device that cannot say what it has gets what the profile names and
  nothing more. Every controller on Linux and on Windows can.

A control that should stay silent says so:

```toml
BTN_TL = "nothing"
```

The factory profile for game controllers is this rule and one line, the
pitch bend on the stick. Every button and every direction of a hat plays a
note, and every other axis sends a controller.

## Layouts and `[global]`

A profile has one or more `[[layout]]` sections. Each has a `name` and a
`[layout.bindings]` table. One layout is selected at a time, and the
`layout_next` and `layout_previous` commands switch between them.

Every layout has all of its bindings, commands included: a key can change
the octave in one layout and play a note in another, and each layout that
should lead to the next says so itself. A key holds on to what it did when
it was pressed: the key that changes the layout does nothing in the new
layout when it is let go of, whatever it does there, and a note held while
the layout changes ends when its key is let go of. The window gives a new
layout the commands of the one it is made from, and the keys that one
leaves to the system or keeps silent; they can be changed apart from then
on.

`[global]` holds one binding, if any: that of `hidi_toggle`, which turns
HIDI off and on the same way in every layout and while HIDI is off. Nothing
else may stand there.

Profiles written for schema 1 shared the rest of `[global]` among the
layouts as well. HIDI still reads them so: every layout gets the bindings
of `[global]` it does not replace with its own. The window writes such a
file in the current form with the first change made to it there, and that
change can be taken back.

## Bindings

A binding is one line: the control on the left, what it does on the right.

### Naming the control

Controls use the Linux input event names on every platform: `KEY_A`,
`BTN_SOUTH`, `ABS_X`. The names describe positions on the device, not the
characters printed on it, so `KEY_Q` is the key to the right of Tab whatever
your keyboard layout is. A control without a name is written with its number,
such as `KEY_0x2f5`.

The name can be extended. A name containing anything but letters, digits and
underscores must be put in quotes.

| Form | Meaning |
|---|---|
| `KEY_A` | The control itself. |
| `"KEY_LEFTSHIFT+KEY_A"` | A combination: `KEY_A` while `KEY_LEFTSHIFT` is held. Any number of keys can be required. When several bindings fit, the one requiring the most held keys is used. |
| `"ABS_Y+"`, `"ABS_Y-"` | The upper or lower half of an axis, starting from its middle. This lets the two directions of a stick or a hat do different things. |
| `"Touchpad:ABS_X"` | A control on a named part of a device that has several, such as the touchpad of a game controller. |

### What it does

The short forms cover the common cases:

```toml
KEY_Z = "C1"            # play a note, by name
KEY_X = 38              # play a note, by number
KEY_F2 = "octave_up"    # run a command
KEY_SPACE = "passthrough"   # leave the key to the system
KEY_F7 = "nothing"      # do nothing
```

**`"nothing"`** makes a control do nothing where something else would give
it a part: in a profile with a `[generate]` rule. A control without any line needs no such
line.

**`"passthrough"`** leaves a key to the rest of the system. It plays nothing,
and it types what it always types, also while HIDI is on and the other keys
of the keyboard do not. That is for the keys the program you play into
needs: the space bar that starts and stops a recording, for one. Only a key
of the main part of a device, pressed alone, can be passed through, and
which keys are follows the layout that is selected.

On Windows a passed key gets through whichever keyboard it is pressed on,
since Windows cannot tell keyboards apart there. On Linux HIDI types the key
again on a keyboard of its own, which needs access to `/dev/uinput` (the
udev rule that comes with HIDI gives it); without that access the key does
nothing while HIDI is on, and HIDI says so in its log.

Note names are a letter, an optional `#` or `b`, and an octave from -2 to 8.
Middle C (note 60) is `C3`.

The long form is a table with exactly one of these settings:

| Setting | Meaning |
|---|---|
| `note = "C1"` | Play a note while the control is pressed. Optional `velocity` (1 to 127) fixes the velocity of this note. Optional `touch = true` takes the velocity from how fast the control is pressed; see below. |
| `cc = 64` | Send a controller, 0 to 127. An axis sends its position. A key sends 127 while held and 0 when released. |
| `pitch_bend = true` | Bend the pitch. A centered axis bends both ways. Anything else bends one way; write `"down"` instead of `true` to bend down. |
| `program = 5` | Send a program change, 0 to 127, when the control is pressed. |
| `command = "panic"` | Run a command; the same as the short form. |

**`touch = true`** on a note makes it louder the faster its control is
pressed. HIDI takes the time the control needs from its rest position to the
point where the note triggers, halfway: 4 ms or less plays at full velocity,
160 ms or more at the lowest, and in between the velocity falls evenly with
each doubling of the time. It is for controls that report how far or how
hard they are pressed: the trigger of a game controller, an analog key, a
pad.

```toml
ABS_RZ = { note = "C1", touch = true }
```

A control that is only on or off has no such time; with `touch` it plays as
without. How finely the time is measured depends on how often the axis is
passed on: with the default `axis_rate` of 120 the steps are about 8 ms, and
`axis_rate = 0` passes on everything the device reports.

and any of these options:

| Option | Meaning |
|---|---|
| `channel_offset` | Added to the current channel, 0 to 15, wrapping around after channel 16. It lets one layout play several channels that move together with the channel commands. |
| `centered` | Set to `true` for an axis that rests in the middle of its travel, such as a stick. The dead zone is then placed around the middle, pitch bend treats the middle as no bend and bends both ways from there, and a controller counts 64 as its rest, to which it returns when a change of channel or layout takes the axis away. Without a dead zone a controller sends the same values either way. Not needed with the `+` and `-` forms. |
| `invert` | Set to `true` to reverse the direction of the control. |
| `deadzone` | The fraction of travel next to the rest position to ignore, from 0 up to 1, for this binding alone. Left out, the axis has the dead zone of the profile (`deadzone` in `[defaults]`). |
| `threshold` | For a note, a program change or a command on an axis: how far the axis has to move for it to trigger, from above 0 to below 1; half of the way unless set. Of the half, for the `+` and `-` forms, and of what is left after the dead zone. The hysteresis of the settings is laid around this point. |

Notes and commands work on axes too: they trigger when the axis passes half of
its travel, or the point `threshold` names:

```toml
ABS_Z = { note = "C3", threshold = 0.2 }                # a trigger as a pad
"ABS_X+" = { command = "octave_up", threshold = 0.8 }   # a stick pushed far right
```

The two halves of an axis can each send a controller of their own, which
is how a stick that springs back to the middle sends 0 at rest and not 64:

```toml
"ABS_RX+" = { cc = 20 }
"ABS_RX-" = { cc = 21 }
```

"Split into two halves" on the page of a profile in the window writes
that.

### Commands

| Command | Effect |
|---|---|
| `octave_up`, `octave_down` | Shift all notes by an octave. |
| `transpose_up`, `transpose_down` | Shift all notes by a semitone. |
| `channel_up`, `channel_down` | Change the MIDI channel. |
| `layout_next`, `layout_previous` | Switch layout. |
| `velocity_up`, `velocity_down` | Change the velocity of the notes that name none of their own, by the step of the settings (`input.velocity_step`). Pressed together, back to the profile's velocity. |
| `panic` | Silence everything on every channel the instrument has played on. |
| `chord` | Remember a chord, or forget it. See below. |
| `learn` | Turn learning mode on, or off: axes stand still until they are moved far. See below. |
| `hidi_toggle` | Turn HIDI off and on, for all instruments. One for a profile, in `[global]`, on a key or on several pressed together. See "One key that turns HIDI off and on". |

Holding both commands of an up/down pair at once resets that value to its
default.

**`chord`** turns one key into a chord. Hold two or more notes and press the
command: HIDI remembers how far the notes are above the lowest of them, and
from then on every note you play sounds together with notes that far above
it. Hold C, E and G, press the command, and every key plays a major chord.
Press the command with fewer than two notes held to forget the chord. The
chord stays through `panic` and through turning HIDI off and on; it is
forgotten when the profile is loaded again.

**`learn`** turns learning mode on, and off again. The mode is for teaching
a program which controller to listen to ("MIDI learn"). Such a program takes
the first controller it hears, and a game controller has many axes that are
touched by accident or do not rest quite still: a change of one step is
enough to be taken.

Learning mode is one for all instruments. When it begins, every axis is
taken to stand where it stands, and what it sends stays exactly there until
the axis has moved by more than half of its range (`learn_threshold` in
the settings). Beyond that it sends where it is, so the program hears just
the one you mean. Back within that distance it sends the place it stood at
again. When the mode ends, every axis that is somewhere else by then sends
where it really is. Keys that send a controller are not held back.

The mode is also turned on and off with the small switch at the top of the
window and from the menu of the icon in the tray, which blinks between green
and yellow while HIDI is on and learning. The keyboard profile that comes
with HIDI has the command on F10.

## What happens while you play

- A note always ends correctly. Changing octave, transposition, channel or
  layout while holding keys does not leave notes hanging.
- When you change channel or layout, pitch bend and the controllers driven by
  keys and centered axes return to rest on the old channel, so nothing is left
  bent or modulated. An axis that is not centered, such as a throttle lever,
  keeps the value it had.
- When HIDI is turned off, every note is released and the pitch bend
  returns to rest; controllers stay as they were last sent. The settings
  `reset_pitch_bend` and `reset_controllers` say otherwise.
- When a device is unplugged, its profile changes or HIDI ends, every note
  is released and every controller and pitch bend returns to rest.
