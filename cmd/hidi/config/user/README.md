To create dedicated configuration for your device type, follow general format
of factory configurations.  
Make sure all sections have proper, space-based indentation.

- `identifier` section is responsible for matching with your device,
  please include proper `bus`, `vendor`, `product` and `version` values
- `uniq` may be useful for user configuration only, when user wants to distinguish devices of the same type
  (device must report that value correctly, most devices doesn't have one, especially keyboards)
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
    Valid range is `0 - 127` or `C-2 - G8`, valid string representation is note name (`CDEFGAB`),
    optional `#` sign for sharp keys (e.g. `C#`) and octave number (`-2 - 8`)
  - Analog gamepad codes - these are identified by `ABS_` prefix.  
    For this kind of events there is a special format that covers few different use cases:
    - `{type: cc, cc: 0}` - CC control for CC0 (for non-negative analog input like trigger)
    - `{type: cc, cc: 0, cc_negative: 1}` - CC control for CC0 and CC1, useful when you want to have
      two different CC messages on positive and negative values
      (like one axis of analog stick with neutral center position)
    - `{type: key, note: c0}` - note emulation, useful for D-pad which is recognized as analog input.
      `note_negative` may be optionally defined as well.
    - `{type: pitch_bend}` - pitch-bend control
    - `{type: action, action: octave_up, action_negative: octave_down}` - self-explanatory (action emulation will be
      moved into `action_mapping` section in the future)
    - for all these types there is optional `flip_axis: true` setting which inverts the interpretation of incoming events.
  - You can see event codes in the debug mode or take look at
    [codes.go](https://github.com/holoplot/go-evdev/blob/c80ef6a93985029e8db7b4a5ca42af976b4ac1a4/codes.go)
    or [input-event-codes.h](https://elixir.bootlin.com/linux/v5.17/source/include/uapi/linux/input-event-codes.h)
    files.
- `deadzones` - todo
- `default_deadzone` - todo
- `collision_mode` - there are two cases when there is a probability of "clashing" midi events.
  first, caused by midi mappings, as user can assign the very same midi note to different hardware keys.
  second, with multi-note mode, as user can play combination of two keys which will share the same midi note.
  there are a few modes available to specify behaviour when note is activated again without releasing it first:
  - `off` - clash is not handled in any way, every press and release will always emit "note on/off" events.
    this may cause premature note deactivation because of previously released key, (not recommended)
  - `no_repeat` - second "note on" event will be not emitted.
    "note off" event will be emitted only when all related keys will be released.
  - `interrupt` - interrupts previously activated note with "note off" event, and then activate it with "note on" again.
  - `retrigger` - doesn't interrupts previously activated note, always emit "note on" events,
    emits "note off" event once all related keys are released. (default).
  
  `interrupt` is a bit more "efficient" than `retrigger`, also it may sound differently (testing needed).
  `retrigger` is recommended as number of "note off" and "note on" events are equal and is guaranteed to work reliably.