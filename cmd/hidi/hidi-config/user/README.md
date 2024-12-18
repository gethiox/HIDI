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
    If your device provides unrecognized key event codes, use hexadecimal notation instead of names
    (for example `x01`, `xff`) 
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
    - for all these types there is optional `flip_axis: true` setting which inverts the interpretation of incoming values, and `deadzone_at_center: true` that sets the deadzone at the center of the range, instead of at zero.
- `deadzones` - key:deadzone mapping in `0.0` - `1.0` range.
- `default_deadzone` - default deadzone value for all other events  that were not specified in `deadzones` section

### Multi Channel mapping

For button presses, CC controls and pitch-bend it is possible to define optional midi channel offset value (0-15 range),
useful to control multiple channels in one mapping.

For button presses, simply put a value after a comma like so:
```toml
KEY_Z = "c0"
KEY_X = "c0,1"
```
In given example, key Z has default channel offset 0 and will emit c0 note at current channel (so first channel by default)
where key X will emit c0 note at current channel + 1 (second channel in that case).

For analog events (CC control and pitch-bend) there are additional fields available to define channel offset: 
`channel_offset` and `channel_offset_negative`. (0 is defined by default)
To specify an offset, simply put a value in respective field like so:
```toml
[mapping.analog]
  ABS_X = { type = "cc", cc = 0 }
  ABS_Y = { type = "pitch_bend", flip_axis = true, channel_offset = 1 }
  ABS_RX = { type = "cc", cc = 1, cc_negative = 2, channel_offset = 2, channel_offset_negative = 3 }
```
In given example, X axis controls CC0 at current channel (first channel), Y axis controls pitch-bend
at current channel + 1 (second channel) and RX axis controls both CC1 and CC2 at current channel + 2 (third channel)
and current channel + 3 (fourth channel) respectively.

#### channel offset behaviour

Device has integrated channel control (up/down), and offset is well integrated with it. When user has defined a mapping
with channel offsets, those parameters will move along with `channel_up`/`channel_down` actions.
For example, key Z has default offset 0 (first channel), key X has offset 1 (second channel), channels will shift when
`channel_up` was triggered, now key Z is at second channel where key X is at third channel.

When channel offset + current channel will exceed expected 1-16 range, it will wrap around back to beginning. 
 
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


# Tip

To ensure your custom configuration is loaded correctly check log output, you should see that device is
loaded/connected with verbose context, for example:
```
Device connected [config=0_default.yaml (user)] [type=Keyboard] [dev=HP, Inc HyperX Alloy Elite 2]
```

Remember that you can freely edit your configuration while app is running,
application will reload all devices and configurations every time change in configurations are detected.
It should be convenient to test your changes in realtime this way.
