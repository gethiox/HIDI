# HIDI

Flexible HID to MIDI translation layer

Demo video:  
[![Beta Demo](./docs/hidi-demo-thumbnail.png)](https://www.youtube.com/watch?v=luA-u8MfgAs)  
Ultimate successor of [keyboard2000](https://github.com/gethiox/keyboard2000)
and [keyboard3000](https://github.com/gethiox/keyboard3000) projects

![Linux](https://img.shields.io/badge/OS-Linux-blue)
![go version](https://img.shields.io/github/go-mod/go-version/gethiox/HIDI)
[![go report](https://goreportcard.com/badge/github.com/gethiox/HIDI)](https://goreportcard.com/report/github.com/gethiox/HIDI)    
![last release](https://img.shields.io/github/v/release/gethiox/HIDI)
![downloads latest](https://img.shields.io/github/downloads/gethiox/HIDI/latest/total)
![downloads](https://img.shields.io/github/downloads/gethiox/HIDI/total)

# Purpose

HIDI is a translation layer between HID devices like keyboards or gamepads and hardware MIDI interface, it also supports ALSA for standalone linux users.  
This way you can play on your **computer keyboard as MIDI device**, use gamepad analog sticks to control
pitch-bend and CC. As many devices as you want, simultaneously.

Easy to use, easy to customize, and it has many quality of life features, carefully crafted with love to provide
the best user experience possible.

- Any number of **customized MIDI mappings**, switchable by dedicated key,
  Piano, Chromatic and Control (every key with unique midi note, useful for DAW control) provided as default configuration
- Gamepad analog input to **control CC, pitch-bend** and note/action emulation
- Several actions like **octave** control (F1-F2), **semitone** (F3-F4), **channel** (F5-F6), **mapping** (F11-F12)
- Action pairs can be pressed at once to reset to default value
- **intelligent note emission logic**, user can freely change device state on the fly 
  (octave, semitone, mapping, channel) even while still pressing keyboard keys, due to careful design
  notes will be not interrupted, will be released correctly on key release and only new key presses will emit
  notes respectively to the new internal state.
- NKRO keyboards support (if it can be enabled in your hardware or enabled by default)
- You can connect as many HID devices as you have free USB slots
- **All devices are loaded/unloaded completely dynamically**
- Application will reload configuration when new one will appear or existing one was changed.
  Very useful when user want to craft their own configuration
- Gyroscope sensor support (arm platforms) for pitch-bend and CC controls
- OpenRGB support ([Demo](https://youtu.be/QF_z6LHcSkE)) (Check `Direct Mode` section [here](https://gitlab.com/CalcProgrammer1/OpenRGB/-/wikis/Supported-Devices#keyboards) for
  supported devices.)

# Requirements

- Machine with Linux, either dedicated one for bridging with hardware MIDI interface or just your local machine
- **Optional MIDI interface**, please avoid cheap china USB interfaces,
  [it has problem with receiving data](http://www.arvydas.co.uk/2013/07/cheap-usb-midi-cable-some-self-assembly-may-be-required/)
  (unless you have old version lying around, it may work just fine).
- **Keyboards**, **gamepads** :)

# Usage

Download the latest version from [releases](https://github.com/gethiox/HIDI/releases) for the platform of your choice.
Place binary on your system and run it with `sudo ./HIDI`.  
See `-h` flag for available optional arguments

- If necessary, add permission for execution with `chmod +x HIDI`
- If you're connected via network to your Pi, it may be useful to run it under **[tmux](https://github.com/tmux/tmux/wiki)**
  to avoid program termination on connection loss, just type `tmux` to run multiplexer, `ctr+b -> d` to leave tmux
  running in the backgroud, `tmux a` to re-enter your session
- During application use you probably don't want to propagate keyboard events into your system  
  To avoid that use `-grab` parameter, use exit sequence (default alt+esc) to terminate program
- ~~Standard user may not have permission to read input devices directly for security reasons.  
  The best way of running this program is to grant temporary privilege to `input` group with:  
  `sudo -u your_username -g input ./HIDI`  
  Try to avoid running untrusted software directly with root privilege~~
  Due to some complications (e.g. OpenRGB root requirement), it's the easiest to run it with `sudo`.
- for standalone linux users, use `-standalone` parameter which preserves one keyboard for user standard input, requires more hardware than one keyboard. `-virtual` parameter will create ready to use ALSA port instead of connecting to existing ports/hardware.
- If you're bridging keyboards with hardware midi interface, see `-listmididevices` for available interfaces and select them with `-mididevice X`

# Configuration

There are two types of configurations:
- `hidi.toml` - minor behavior settings
- device configurations, see [guide](cmd/hidi/hidi-config/user/README.md) for details.

Have fun!

## Development

### Building

Make sure you have `go >= 1.18` installed  
run `go run build.go -cgo -platforms linux-amd64`  
By default, it will build all defined platforms without OpenRGB support,
to select specific platforms or enable OpenRGB (requires precompiled binaries), see `go run build.go -h` for usage

### Wishlist

Features that are possible to achieve. With enough interest and support I may be motivated to implement these.

- configurable modifier key/keys for expanded mapping (key sequence like `modifier+KEY_A`)
- ~~localhost mode for Linux users without requirement of separate machine (jack/alsa)~~ done!
- Network MIDI
- Bluetooth MIDI device
- Arpeggiator (with MIDI clock sync), note latch, multinote MIDI effects
- Fully featured DAW control plugins
- standalone, fully featured **MIDI sequencer** with internal and external midi input support.
  Ideal feature for OpenRGB devices.

Do you like some specific idea listed above? Have you some improvement/feature idea not listed here?  
Feel free to leave your wishes in `Discussions` section!

# Contribution

Any kind of help is highly appreciated!  

### Features

You can propose features and improvements. There is no guarantee that I'm going to implement it right-away.
It depends on the complexity of given feature and overall integrity with the rest of application,
it depends on my free time and free will as well.

### Bugs

If you faced a bug, instability or general problems with application, feel free to open issue
and provide some information like error messages and logs.  
In the case of problems with your specific HID devices,
please provide the full content of `/proc/bus/input/devices` file and your platform.

### Code

Feel free to open pull requests.

### Configurations

If your keyboard doesn't work correctly with default configuration,
create your own and add it in a pull request as `Factory` configuration.
Make sure it has proper values in `Identifier` section and loads correctly.  
For more information, see [guide](cmd/hidi/hidi-config/user/README.md)

Please make sure that all default mappings are working and arranged correctly.
(Piano, Chromatic, Control - this one covers all keyboard keys with unique notes).    

### My questions

- Negotiating/enabling N-Key Rollover keyboard mode from the OS side. Currently, NKRO is supported for devices that
  have ability to enforce that with a key combination. If you may have an idea how to do it, please lave your answer
  [here](https://unix.stackexchange.com/questions/675933/keyboard-input-n-key-rollover).

# License

Project is released under **GPLv3**, for detailed information see [LICENSE](./LICENSE)
