# Context
To create dedicated configuration for your device, please follow general format of factory configurations.

To override given factory configuration, simply create a copy from `factory` into `user` directory.
To create user-defined default configuration, keep `identifier` section with zero values.
Any other user configuration with defined `identifier` section will override default one if such identifier is detected.

Please make sure all sections have proper, space-based indentation. Application will try to report meaningful errors
when problems with configuration file will occur.

Key/Analog definition for `action_mapping` and `midi_mappings` uses direct linux naming format, you can get these
by using `Debug` mapping with `-loglevel 3` parameter, example output:
```
Undefined KEY event: type: 0x01 [EV_KEY], code: 0x71 [KEY_MUTE/KEY_MIN_INTERESTING], value: 1 [event3] [dev=HP, Inc HyperX Alloy Elite 2]
```
If there is alternative names available (like above `KEY_MUTE` and `KEY_MIN_INTERESTING`), just pick one.
You can also take a look at [codes.go](https://github.com/holoplot/go-evdev/blob/c80ef6a93985029e8db7b4a5ca42af976b4ac1a4/codes.go)
or [input-event-codes.h](https://elixir.bootlin.com/linux/v5.17/source/include/uapi/linux/input-event-codes.h)
files.

# Field description

### General

- `identifier` section is responsible for matching config with your device,
  please include proper `bus`, `vendor`, `product` and `version` values, keep hexadecimal format with `0x` prefix.  
  (values can be found with `cat /proc/bus/input/devices`)
- `uniq` (optional) may be useful for user configurations only, when user wants to distinguish devices of the same type
  (device must report that value correctly, most devices doesn't have one, especially keyboards)
- `defaults` - defines default device state for given parameter:
  - `octave`, `semitone`
  - `channel` - range 1-16
  - `mapping` - mapping name included in `midi_mappings`
- `collision_mode` - there is a possibility of "clashing" midi events caused by midi mappings,
  as user can assign the very same midi note to different hardware keys, and the press it at once.
  there are a few modes available to specify behaviour when note is activated again without releasing it first:
  - `off` - most primitive behavior where clash is not handled in any way, every press and release will always
    emit "note on/off" events. this may cause premature note deactivation (not recommended)
  - `no_repeat` - doesn't emit "note on" event when note is already active.
    "note off" event will be emitted only when all related keys will be released.
  - `interrupt` - always interrupts previously activated note with "note off" event,
    and then activate it with "note on" again, emits "note off" event once when all related keys are released. (default)
  - `retrigger` - doesn't interrupt previously activated note, always emit "note on" events,
    emits "note off" event once when all related keys are released.

### Key/Analog Mapping

- `action_mapping` - self-explanatory, currently supported actions:
  - `mapping_up`
  - `mapping_down`
  - `octave_up`
  - `octave_down`
  - `semitone_up`
  - `semitone_down`
  - `channel_up`
  - `channel_down`
  - `multinote`
  - `panic`
  - `cc_learning`
- `midi_mappings` - this is where you're defining key:note relationship. Each mapping have its own
  unique name, and corresponding key:value dictionary.
  - Key event codes - these are identified by `KEY_` and `BTN_` prefixes.    
    You can assign note value only for those, in the integer number or string representation.  
    Valid range is `0` - `127` or `C-2` - `G8`, valid string representation is note name (`CDEFGAB`),
    optional `#` sign for sharp keys (e.g. `C#`) and octave number (`-2` - `8`)
  - Analog gamepad codes - these are identified by `ABS_` prefix.  
    For this kind of events there is a special format that covers few different use cases:
    - `{type: cc, cc: 0}` - CC control for CC0 (for non-negative analog input like trigger)
    - `{type: cc, cc: 0, cc_negative: 1}` - CC control for CC0 and CC1, useful when you want to have
      two different CC messages for positive and negative values
      (like one axis of analog stick with neutral center position)
    - `{type: key, note: c0}` - note emulation, useful for D-pad which is recognized as analog input.
      `note_negative` may be optionally defined as well.
    - `{type: pitch_bend}` - pitch-bend control
    - `{type: action, action: octave_up, action_negative: octave_down}` - self-explanatory (action emulation will be
      moved into `action_mapping` section in the future)
    - for all these types there is optional `flip_axis: true` setting which inverts the interpretation of incoming values.
- `deadzones` - key:deadzone mapping in `0.0` - `1.0` range.
- `default_deadzone` - default deadzone value for all other events  that were not specified in `deadzones` section

### OpenRGB

- `open_rgb`: main configuration section
  - `colors` - MIDI-related LED color configuration.  
    each unit defined with 24-bit integer, preferably in hexadecimal format like 0xffab00 (RGB).
    - `white` - white notes
    - `black` - black notes
    - `c` - C notes
    - `unavailable` - all unavailable keys
    - `other` - all other LEDs supported by keyboard but not used by application, eg. additional LED strip
    - `active` - notes pressed on keyboard directly
    - `active_external` - notes enabled by external midi input on current channel

### Gyro

Gyro requires hardware i2c sensor and configuration in `hidi.toml` to work properly.

You can configure several different keys for different CC values, even when using
the same axis, just fill up another `[[gyro]]` section

- `gyro`: main configuration section
  - `type` - `cc` or `pitch_bend`
  - `cc` - ControlChange number (ignored for `pitch_bend`)
  - `activation_key` - keyboard activation key
  - `activation_mode` - `toggle` or `hold`
  - `axis` - `x`, `y` or `z`, respective gyro axis
  - `reset_on_deactivation` - bool, set value back to 0 on release/deactivation
  - `flip_axis` - flip direction
  - `value_multipier` - float, strength setting on axis rotation


# Tip

To ensure your custom configuration is loaded correctly check log output, you should see that device is
loaded/connected with verbose context, for example:
```
Device connected [config=0_default.yaml (user)] [type=Keyboard] [dev=HP, Inc HyperX Alloy Elite 2]
```

Remember that you can freely edit your configuration while app is running,
application will reload all devices and configurations every time change in configurations are detected.
It should be convenient to test your changes in realtime this way.