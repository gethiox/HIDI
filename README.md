# HIDI
Flexible HID to MIDI translation layer
![no china usb midi](./docs/hidi-logo.png)
Ultimate successor of [keyboard2000](https://github.com/gethiox/keyboard2000)
and [keyboard3000](https://github.com/gethiox/keyboard3000) projects.

Demo video:  
[![Beta Demo](./docs/hidi-demo-thumbnail.png)](https://www.youtube.com/watch?v=luA-u8MfgAs)

# Purpose of this project
This application is a translation layer between HID devices like keyboards or gamepads and MIDI interface 
with a bunch of useful features:

- Any number of customized MIDI mappings, easily switchable by a precise binding, currently included Piano
  and Accordion (chromatic layout like lumatone or LinnStrument)
- Making use of analog gamepad input to control things such as pitch-bend or CC (also configurable).
- octave (F1-F2), semitone (F3-F4), mapping (F5-F6) channel (F7-F8) controls.
- customizable multinote mode with just one press of a button, simply hold any number of desired additional intervals
  and press F9. Press again without holding any notes to disable multinote mode.
- non-lazy note emitting implementation, user can conveniently change device state on the fly (octave, semitone, channel)
  even when some notes are already pressed, NoteOff events will be emitted correctly anyway. However, panic button (ESC)
  is also available just in case.
- NKRO keyboards support (if it can be enabled in hardware by some key-sequence)
- You can connect whatever number of HID devices you want, completely dynamically!

# Initial status of the project
It is just in usable state as a beta release, but already feature-rich and stable.  
There is a small list of missing functionality that I want to implement:
- YAML configurations for devices, currently configurations are hardcoded and can be only changed by modifying the code.
- Arpeggiator and other MIDI effects, MIDI clock sync
- a few tiny refactorizations, implement missing features in [holoplot's go-evdev](https://github.com/holoplot/go-evdev) library,
  other related upkeep tasks in the codebase
- performance improvements in the field of monitoring for connected devices, currently `/proc/bus/input/devices` is being
  parsed every second, it can be implemented much more efficiently
- Precompiled builds targeted for more platforms
- MIDI sequencer, only if there are keyboards available with full LED control over Linux input subsystem (wishlist)  

# Requirements
- **Application is designed to be run under a Linux machine**, it can be run under Raspberry Pi zero,
  it can be run on x64 dedicated tower PC
- In the case of Pi Zero, thing like USB HAT may be very useful
- **decent MIDI interface**, please avoid cheap china USB interfaces, [it has problem with receiving data](http://www.arvydas.co.uk/2013/07/cheap-usb-midi-cable-some-self-assembly-may-be-required/)
  (unless you have old version lying around, it may work just fine). Here is my recommendation:
  ![no china usb midi](./docs/no-china-usb-midi.png)
- If you don't have spare MIDI ports on your PC, two identical USB MIDI interfaces with some DIN 5p bridges may be useful
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

example stdout:
```
2021/12/25 10:18:39 New Devices: 3
2021/12/25 10:18:39 - [Keyboard], "Kingston HyperX Alloy FPS Mechanical Gaming Keyboard", 5 handlers
2021/12/25 10:18:39 - [Keyboard], "HOLTEK USB-HID Keyboard", 6 handlers
2021/12/25 10:18:39 - [Joystick], "Microsoft X-Box One S pad", 1 handlers
2021/12/25 10:18:45 > mapping up (Accordion) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:50 > Note On : D  1 (channel:  1, velocity:  64) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:50 > Note Off: D  1 (channel:  1, velocity:   0) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:50 > Note On : D# 1 (channel:  1, velocity:  64) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:51 > Note Off: D# 1 (channel:  1, velocity:   0) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:51 > Note On : E  1 (channel:  1, velocity:  64) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:51 > Note Off: E  1 (channel:  1, velocity:   0) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:51 > Note On : F  1 (channel:  1, velocity:  64) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:51 > Note Off: F  1 (channel:  1, velocity:   0) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:54 > octave up (1) [HOLTEK USB-HID Keyboard]
2021/12/25 10:18:56 > semitone up (1) [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:00 > semitone up (2) [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:08 > Note On : B  1 (channel:  1, velocity:  64) [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:08 > Note On : B  2 (channel:  1, velocity:  64) [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:09 > Multinote mode engaged, intervals: [12]/[Perfect octave] [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:13 > Note Off: B  1 (channel:  1, velocity:   0) [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:13 > Note Off: B  2 (channel:  1, velocity:   0) [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:13 > Note Off: B  2 (channel:  1, velocity:   0) [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:13 > Note Off: B  3 (channel:  1, velocity:   0) [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:17 > Bruh, no pressed notes, multinote mode disengaged [HOLTEK USB-HID Keyboard]
2021/12/25 10:19:44 > Note On : C  1 (channel:  1, velocity:  64) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:19:44 > Note Off: C  1 (channel:  1, velocity:   0) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:19:49 > octave up (1) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:19:49 > Note On : C  2 (channel:  1, velocity:  64) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:19:50 > Note Off: C  2 (channel:  1, velocity:   0) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:19:57 > Note On : D  0 (channel:  1, velocity:  64) [Microsoft X-Box One S pad]
2021/12/25 10:19:57 > Note Off: D  0 (channel:  1, velocity:   0) [Microsoft X-Box One S pad]
2021/12/25 10:19:59 > Note On : C  0 (channel:  1, velocity:  64) [Microsoft X-Box One S pad]
2021/12/25 10:19:59 > Note Off: C  0 (channel:  1, velocity:   0) [Microsoft X-Box One S pad]
2021/12/25 10:20:06 > Control Change:   4, value:  20 (channel:  1) [Microsoft X-Box One S pad]
2021/12/25 10:20:06 > Control Change:   4, value:  87 (channel:  1) [Microsoft X-Box One S pad]
2021/12/25 10:20:06 > Control Change:   4, value: 127 (channel:  1) [Microsoft X-Box One S pad]
2021/12/25 10:20:08 > Control Change:   4, value:  92 (channel:  1) [Microsoft X-Box One S pad]
2021/12/25 10:20:08 > Control Change:   4, value:  17 (channel:  1) [Microsoft X-Box One S pad]
2021/12/25 10:20:08 > Control Change:   4, value:   0 (channel:  1) [Microsoft X-Box One S pad]
2021/12/25 10:21:45 > Note On : D# 1 (channel:  2, velocity:  64) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:21:46 > channel up ( 3) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:21:48 > Note Off: D# 1 (channel:  2, velocity:   0) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:21:50 > Note On : D# 1 (channel:  3, velocity:  64) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
2021/12/25 10:21:51 > Note Off: D# 1 (channel:  3, velocity:   0) [Kingston HyperX Alloy FPS Mechanical Gaming Keyboard]
```

Have fun!