# Profile files

A profile describes one instrument: which devices it applies to, the state it
starts in, and what each control does. Profiles are [TOML](https://toml.io)
files.

HIDI ships with built-in profiles for keyboards and game controllers, so a
device works without any configuration. The built-in files are in
[`internal/config/builtin`](../internal/config/builtin) and are the best
starting point for your own.

HIDI checks a profile completely when it loads it and reports every problem it
finds at once.

## Where your profiles live

HIDI reads every `.toml` file in its profile directory:

| System | Directory |
|---|---|
| Linux | `~/.config/hidi/profiles` (or under `$XDG_CONFIG_HOME`) |
| Windows | `%AppData%\hidi\profiles` |

The `-profiles DIR` option of every command names another directory, and
`-profiles ""` uses the built-in profiles only.

```
hidi profiles            list the profiles, yours first, and check your files
hidi profiles -export    write the built-in profiles into the directory
```

`hidi profiles -export` gives you the built-in profiles as files to edit. It
never overwrites a file that is already there.

When several profiles select a device, the one that describes it most closely
wins: a profile for one product beats one for all keyboards. Between two that
are equally close, yours beats the built-in one, and among your own the file
that sorts later by name wins.

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
usually the built-in one. `hidi profiles` shows the same report and exits
with status 1, which makes it usable as a check.

## Example

```toml
schema = 1
name = "My keyboard"

[match]
type = "keyboard"

[defaults]
channel = 1
velocity = 100
layout = "Piano"

[global]
KEY_F1 = "octave_down"
KEY_F2 = "octave_up"
"KEY_LEFTSHIFT+KEY_F1" = "transpose_down"
KEY_ESC = "panic"

[[layout]]
name = "Piano"

[layout.bindings]
KEY_Z = "C1"
KEY_S = "C#1"
KEY_X = "D1"
KEY_SPACE = { cc = 64 }
```

## Top level

| Setting | Meaning |
|---|---|
| `schema` | Required. The version of this format the file is written for. The current version is `1`. HIDI reads files written for older versions and upgrades them as it loads them. |
| `name` | A name for the profile, shown in logs and the user interface. |

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

Collision modes:

| Mode | A second press of a sounding note | The note ends when |
|---|---|---|
| `interrupt` | ends the note and starts it again | the last control holding it is released |
| `no_repeat` | does nothing | the last control holding it is released |
| `retrigger` | starts it again without ending it first | the last control holding it is released |
| `off` | starts it again without ending it first | any control holding it is released |

## Layouts and global bindings

A profile has one or more `[[layout]]` sections. Each has a `name` and a
`[layout.bindings]` table. One layout is selected at a time, and the
`layout_next` and `layout_previous` commands switch between them.

Bindings in `[global]` apply in every layout. If a layout binds the same
control, the layout's binding is used.

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
```

Note names are a letter, an optional `#` or `b`, and an octave from -2 to 8.
Middle C (note 60) is `C3`.

The long form is a table with exactly one of these settings:

| Setting | Meaning |
|---|---|
| `note = "C1"` | Play a note while the control is pressed. Optional `velocity` (1 to 127) fixes the velocity of this note. |
| `cc = 64` | Send a controller, 0 to 127. An axis sends its position. A key sends 127 while held and 0 when released. |
| `pitch_bend = true` | Bend the pitch. A centered axis bends both ways. Anything else bends one way; write `"down"` instead of `true` to bend down. |
| `program = 5` | Send a program change, 0 to 127, when the control is pressed. |
| `command = "panic"` | Run a command; the same as the short form. |

and any of these options:

| Option | Meaning |
|---|---|
| `channel_offset` | Added to the current channel, 0 to 15, wrapping around after channel 16. It lets one layout play several channels that move together with the channel commands. |
| `centered` | Set to `true` for an axis that rests in the middle of its travel, such as a stick. The dead zone is then placed around the middle, and pitch bend treats the middle as no bend. Not needed with the `+` and `-` forms. |
| `invert` | Set to `true` to reverse the direction of the control. |
| `deadzone` | The fraction of travel next to the rest position to ignore, from 0 up to 1. |

Notes and commands work on axes too: they trigger when the axis passes half of
its travel.

### Commands

| Command | Effect |
|---|---|
| `octave_up`, `octave_down` | Shift all notes by an octave. |
| `transpose_up`, `transpose_down` | Shift all notes by a semitone. |
| `channel_up`, `channel_down` | Change the MIDI channel. |
| `layout_next`, `layout_previous` | Switch layout. |
| `velocity_up`, `velocity_down` | Change the note velocity. |
| `panic` | Silence everything on every channel the instrument has played on. |

Holding both commands of an up/down pair at once resets that value to its
default.

## What happens while you play

- A note always ends correctly. Changing octave, transposition, channel or
  layout while holding keys does not leave notes hanging.
- When you change channel or layout, pitch bend and the controllers driven by
  keys and centered axes return to rest on the old channel, so nothing is left
  bent or modulated. An axis that is not centered, such as a throttle lever,
  keeps the value it had.
- When an instrument is disabled or its device is unplugged, every note is
  released and every controller and pitch bend returns to rest.
