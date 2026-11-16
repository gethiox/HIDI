# HIDI

Flexible HID to MIDI translation layer: play your computer keyboard as a MIDI
instrument, and use game controllers for notes, pitch bend and control changes.

![HIDI logo](./docs/hidi-logo.png)

> **This is the development branch for HIDI 2.0.** It is a rebuild in
> progress: it can be tried on Linux, as described below, but is far from
> complete. For the released version, see the
> [`main` branch](https://github.com/gethiox/HIDI/tree/main) and the
> [releases page](https://github.com/gethiox/HIDI/releases).

## What 2.0 changes

- Runs on Linux and Windows 11 (25H2 or later).
- Every input device becomes its own MIDI device, which appears and disappears
  with the hardware.
- A terminal interface for headless use, plus a tray icon and a graphical
  interface.
- A new configuration format. Configuration files from HIDI 1.x are not
  compatible.

The structure is described in [docs/architecture.md](docs/architecture.md), and what is
done and what remains in [docs/roadmap.md](docs/roadmap.md).
The profile file format is described in [docs/configuration.md](docs/configuration.md).

## Building

HIDI needs Go 1.27 or later and no C toolchain.

```bash
go build ./cmd/hidi
```

Cross-compiling works with the usual Go variables, for example:

```bash
GOOS=windows GOARCH=amd64 go build ./cmd/hidi
```

## Trying it on Linux

HIDI reads input devices from `/dev/input` and creates MIDI devices through
the ALSA sequencer. Your user needs access to both, which on most
distributions means being a member of the `input` and `audio` groups:

```bash
sudo usermod -aG input,audio "$USER"
```

Log out and back in for the change to take effect.

List the input devices HIDI sees and the profile each would use:

```bash
./hidi devices
```

Run the instruments:

```bash
./hidi run
```

Every keyboard and game controller becomes a MIDI device named `HIDI`
followed by the device's name. It shows up in your DAW or synthesizer, and
in the output of `aconnect -l`. Unplugging the device removes it.

While HIDI is on, keyboards are taken away from the rest of the system, so
that playing does not type. That includes the keyboard you started HIDI
with. To get it back, turn HIDI off with the toggle combination:

**Left Ctrl + Left Shift + Left Alt**, pressed together.

Pressing it again turns HIDI back on. While HIDI is off the instruments are
silent, and the MIDI devices stay in place. To stop HIDI, turn it off and
press Ctrl+C. Game controllers are not taken away, so they keep working in
other programs.

| Option | Effect |
|---|---|
| `-toggle KEYS` | Use another combination, such as `-toggle KEY_SCROLLLOCK` or `-toggle KEY_LEFTCTRL+KEY_RIGHTCTRL`. |
| `-no-grab` | Leave keyboards usable by the system; they type while you play. |
| `-grab-gamepads` | Also take game controllers away from other programs. |
| `-off` | Start turned off. |
| `-v` | Also log every MIDI message and every ignored device. |
| `-no-color` | Write the log without colours. |
| `-profiles DIR` | Read your profiles from another directory. |
| `-connect PORT` | Also send to an existing MIDI port; see below. |

The log is coloured when it goes to a terminal, and plain when it goes to a
file or a pipe. Besides `-no-color`, setting the `NO_COLOR` environment
variable turns the colours off; `FORCE_COLOR` keeps them when the output is
piped.

To change what a device plays, run `./hidi profiles -export` and edit the
files it writes to `~/.config/hidi/profiles`. HIDI picks up changes while it
runs. The [profile format](docs/configuration.md) is documented.

### Looking at a running HIDI

From another terminal:

```
./hidi status     # the instruments and their state
./hidi monitor    # the same, kept up to date, with the end of the log
./hidi off        # turn HIDI off, like the toggle combination
./hidi on
./hidi toggle
```

`hidi off` is useful as a desktop shortcut or from a script. `hidi monitor`
redraws in place on a terminal; piped, or with `NO_COLOR` set, it writes
each change below the last.

### Sending to a hardware MIDI interface

Every instrument is a MIDI device of its own that other programs connect to.
HIDI can also send straight to a MIDI port that already exists, such as a
USB MIDI interface with a synthesizer behind it, which is what a single-board
computer without a screen needs:

```
./hidi ports                      # what is there
./hidi run -connect "USB MIDI"    # send every instrument there as well
```

`-connect` takes a part of the port's name, or its ID from the first column
of `hidi ports`, and may be given several times. The port does not have to
exist yet: HIDI connects when it appears, and again after it was unplugged.

The default keyboard profile plays notes on the letter rows, changes octave
with F1 and F2, transposes with F3 and F4, changes channel with F5 and F6,
and switches layout with F11 and F12. Escape silences everything.

## Trying it on Windows

Only MIDI output exists on Windows so far. It has created a device and sent
to it on one machine; expect rough edges. `hidi doctor` is the way to find out
whether it works on yours:

```
hidi.exe doctor
```

It prints the Windows build, where it found Windows MIDI Services, and each
stage of creating a MIDI device. If all stages pass, a device named
"HIDI test device" appears in other programs and plays a C major chord until
you press Ctrl+C. If a stage fails, or the program stops without a message,
the output up to that point says where.

Windows MIDI Services needs Windows 11 25H2 or later. Until Windows itself
ships the API, `Windows.Devices.Midi2.dll` has to be supplied. Take it, with
the `.pri` file next to it, from the NuGet package
`Windows.Devices.Midi2.<version>.nupkg` on the releases page of
[microsoft/MIDI](https://github.com/microsoft/MIDI/releases); the package is
a zip archive. Do not use the copy that Microsoft's Tools installer places
next to its own programs. The DLL has to be
in the same folder as `hidi.exe`, unless `hidi doctor` reports that it found
the API in Windows. A program started with `go run` lives in a temporary
folder, so either build it first (`go build -o hidi.exe ./cmd/hidi`) or set
the environment variable `HIDI_MIDI_DLL` to the full path of the DLL.

## Development

- `main` holds the released 1.x code and is not changed until 2.0 is ready.
- `dev` is the integration branch for 2.0. Changes reach it only through pull
  requests.
- Every pull request must pass CI: `gofmt`, `go vet`, `staticcheck`, the tests,
  and builds for all supported targets.
- The tests that need the kernel's input and MIDI interfaces run in a virtual
  machine in CI. They include an end-to-end test (`internal/e2e`) that starts
  the real program, plays a keyboard created in software and checks the MIDI
  that arrives. On a Linux machine with `/dev/uinput` and the ALSA sequencer
  they also run locally, as root or with access to both:
  `go build -o /tmp/hidi ./cmd/hidi && HIDI_E2E_BINARY=/tmp/hidi go test ./internal/e2e`
- `scripts/vm-test.sh` runs those tests in a virtual machine of its own, for a
  machine or container without the two interfaces. It needs no hardware
  virtualisation; the comment at its top lists the packages.

Run the same checks locally before opening a pull request:

```bash
gofmt -l .
```

```bash
go vet ./...
```

```bash
go test -race ./...
```

## License

HIDI is released under **GPLv3**, see [LICENSE](./LICENSE).
