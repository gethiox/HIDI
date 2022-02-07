# HIDI
Flexible HID to MIDI application
![no china usb midi](./docs/hidi-logo.png)

# Purpose of this project
This application is a translation layer between HID devices like keyboards or gamepads and MIDI interface 
with a bunch of useful features:

- Any number of customized MIDI mappings, easily switchable by a precise binding (currently included Piano and Accordion)
- Making use of analog gamepad input to control things such as pitch-bend or CC (also configurable).
- octave (F1-F2), semitone (F3-F4), mapping (F5-F6) channel (F7-F8) controls.
- customizable multinote mode with just one press of a button, simply hold any number of desired additional intervals
  and press F9. Press again without holding any notes to disable multinote mode.
- non-lazy note emitting implementation, user can conveniently change device state on the fly (octave, semitone, channel)
  even when some notes are already pressed, NoteOff events will be emitted correctly anyway. However, panic button (ESC)
  is also available just in case.
- You can connect whatever number of HID devices you want, completely dynamically!

# Initial status of the project
It is just in usable state as a beta release, but already feature-rich and stable enough in my opinion.  
There is a small list of missing functionality that I want to implement:
- YAML configurations for devices, currently configurations are hardcoded and can be only changed by modifying the code.
- Arpeggiator and other MIDI effects, MIDI clock sync
- a few tiny refactorizations, implement missing features in [holoplot's go-evdev](https://github.com/holoplot/go-evdev) library,
  other related upkeep tasks in the codebase
- performance improvements in the field of monitoring for connected devices, currently `/proc/bus/input/devices` is being
  parsed every second, it can be implemented much more efficiently
- Precompiled builds targeted for more platforms
- MIDI sequencer, only if there are Keyboards available with full LED control over Linux input subsystem (wishlist)  

# Requirements
- **Application is designed to be run under a Linux machine**, it can be run under Raspberry Pi zero,
  it can be run on x64 dedicated tower PC
- In the case of Pi Zero, thing like USB HAT may be very useful
- **decent MIDI interface**, please avoid cheap china USB interfaces, [it has problem with receiving data](http://www.arvydas.co.uk/2013/07/cheap-usb-midi-cable-some-self-assembly-may-be-required/)
  (unless you have old version lying around, it may work)
  ![no china usb midi](./docs/no-china-usb-midi.png)
- **Keyboards**, **gamepads** :)

# Usage
- `./build.sh` provides a simple way to compile program for Raspberry Pi zero, beta release is also available to download.
- `./hidi -h` for available options:
```
Usage of ./hidi:
  -debug
        enable debug logging
  -mididevice int
        select N-th midi device, default: 0 (first)
```
- if necessary, add permission for execution with `chmod +x hidi`
- just run by `./hidi`

Have fun!