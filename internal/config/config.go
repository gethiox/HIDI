package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
)

// DefaultVelocity is the note velocity used when a profile does not set one.
const DefaultVelocity = 100

// Profile is one parsed profile file.
type Profile struct {
	// Name is a human-readable name for the profile.
	Name string
	// Match selects the devices the profile applies to.
	Match Match
	// Engine is the instrument as the file writes it out. A profile with
	// a rule for the controls it does not name describes more than that,
	// and what depends on the device: see [Profile.For].
	Engine engine.Profile
	// Generate is that rule; the zero value is none.
	Generate Generate
	// Toggle is the key, or the keys pressed together, that turn HIDI off
	// and on from a device with this profile: those of the binding of
	// "hidi_toggle". Empty where the profile has none.
	Toggle []control.Code
	// WrittenFor is the schema version the file declared. It is lower than
	// [Schema] if the file was upgraded while reading.
	WrittenFor int
	// AxisRate is how many updates a second an absolute axis may pass on
	// to the engine. Zero means every one.
	AxisRate int
	// AxisThreshold is how far a resting axis has to move, as a fraction
	// of its travel, before its positions are passed on. Zero passes
	// everything.
	AxisThreshold float64
	// Source says where the profile comes from: the path of its file, or
	// "built-in/" and a file name. [Parse] leaves it empty.
	Source string
	// Kind tells the profiles HIDI comes with from the user's own:
	// [KindFactory] or [KindUser]. Whoever loads the profile sets it.
	Kind string
	// Enabled is false for a profile that says "enabled = false": it is
	// kept and not used. That is how a profile of the user's steps back
	// for the factory profile it stands in for.
	Enabled bool
	// Digest identifies the content of the file. Two profiles with the same
	// digest behave the same.
	Digest [sha256.Size]byte
}

// The kinds of profile.
const (
	KindFactory = "factory"
	KindUser    = "user"
)

// BuiltinSource starts the [Profile.Source] of the profiles inside HIDI.
const BuiltinSource = "built-in/"

// IsBuiltin reports whether the profile ships inside HIDI.
func (p *Profile) IsBuiltin() bool { return strings.HasPrefix(p.Source, BuiltinSource) }

// Match selects devices. Every field that is set must agree with the device;
// a Match with no fields set selects every device.
type Match struct {
	// Type restricts the profile to one class of device.
	// control.DeviceUnknown means any.
	Type control.DeviceType
	// Bus, Vendor, Product and Version restrict the profile to devices
	// reporting these hardware identifiers. Nil means any.
	Bus, Vendor, Product, Version *uint16
	// Serial restricts the profile to the one device with this serial
	// number. Empty means any.
	Serial string
}

// String describes the devices the match selects, for listings.
func (m Match) String() string {
	var parts []string
	if m.Type != control.DeviceUnknown {
		parts = append(parts, "type="+m.Type.String())
	}
	for _, id := range []struct {
		name  string
		value *uint16
	}{{"bus", m.Bus}, {"vendor", m.Vendor}, {"product", m.Product}, {"version", m.Version}} {
		if id.value != nil {
			parts = append(parts, fmt.Sprintf("%s=0x%04x", id.name, *id.value))
		}
	}
	if m.Serial != "" {
		parts = append(parts, fmt.Sprintf("serial=%q", m.Serial))
	}
	if len(parts) == 0 {
		return "any device"
	}
	return strings.Join(parts, " ")
}

// Score reports whether the device is selected and, if so, how specific the
// match is: the number of fields that were set. When several profiles select
// a device, the one with the highest score describes it most closely.
func (m Match) Score(d control.DeviceInfo) (score int, ok bool) {
	if m.Type != control.DeviceUnknown {
		if m.Type != d.Type {
			return 0, false
		}
		score++
	}
	for _, f := range []struct {
		want *uint16
		got  uint16
	}{
		{m.Bus, d.Hardware.Bus}, {m.Vendor, d.Hardware.Vendor},
		{m.Product, d.Hardware.Product}, {m.Version, d.Hardware.Version},
	} {
		if f.want == nil {
			continue
		}
		if *f.want != f.got {
			return 0, false
		}
		score++
	}
	if m.Serial != "" {
		if m.Serial != d.Serial {
			return 0, false
		}
		score++
	}
	return score, true
}

// Parse reads a profile file. The returned error describes every problem
// found in the file, one per line.
func Parse(data []byte) (*Profile, error) {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		var derr *toml.DecodeError
		if errors.As(err, &derr) {
			line, column := derr.Position()
			return nil, fmt.Errorf("line %d, column %d: %s", line, column, derr.Error())
		}
		return nil, err
	}
	from, err := migrate(doc, migrations, Schema)
	if err != nil {
		return nil, err
	}

	r := &reader{}
	p := &Profile{WrittenFor: from, Digest: sha256.Sum256(data)}
	top := r.table("", doc, "schema", "name", "enabled", "match", "defaults", "generate", "global", "layout")
	p.Name = top.str("name", "")
	p.Enabled = top.boolean("enabled", true)
	p.Match = r.match(top.sub("match"))
	var s settings
	p.Engine.Defaults, s = r.defaults(top.sub("defaults"))
	p.Engine.Collision, p.Engine.RestAxes, p.AxisRate, p.AxisThreshold = s.collision, s.restAxes, s.axisRate, s.axisThreshold
	p.Generate = r.generate(top.sub("generate"))
	p.Engine.Global = r.bindings("global", top.sub("global").values)

	layouts, _ := top.values["layout"].([]any)
	if _, isList := top.values["layout"].([]any); top.has("layout") && !isList {
		r.failf("layout", "must be written as [[layout]] sections")
	}
	for i, raw := range layouts {
		where := fmt.Sprintf("layout %d", i+1)
		values, ok := raw.(map[string]any)
		if !ok {
			r.failf(where, "must be a table, not %s", describe(raw))
			continue
		}
		t := r.table(where, values, "name", "bindings")
		name := t.str("name", "")
		if name != "" {
			where = fmt.Sprintf("layout %q", name)
			t.where = where
		}
		p.Engine.Layouts = append(p.Engine.Layouts, engine.Layout{
			Name:     name,
			Bindings: r.bindings(where, t.sub("bindings").values),
		})
	}

	p.Toggle = r.toggle(&p.Engine)

	// The engine checks everything that depends on values rather than on
	// the shape of the file.
	if err := p.Engine.Validate(); err != nil {
		r.errs = append(r.errs, err)
	}
	if len(r.errs) > 0 {
		return nil, errors.Join(r.errs...)
	}
	return p, nil
}

func (r *reader) match(t *table) Match {
	var m Match
	switch typ := t.str("type", ""); typ {
	case "":
	case "keyboard":
		m.Type = control.DeviceKeyboard
	case "gamepad":
		m.Type = control.DeviceGamepad
	case "mouse":
		m.Type = control.DeviceMouse
	default:
		r.failf(t.where, "type %q is not one of keyboard, gamepad, mouse", typ)
	}
	m.Bus = t.id("bus")
	m.Vendor = t.id("vendor")
	m.Product = t.id("product")
	m.Version = t.id("version")
	m.Serial = t.str("serial", "")
	t.done("type", "bus", "vendor", "product", "version", "serial")
	return m
}

// AxisRate limits, and the default rate at which an absolute axis is passed
// on, in updates a second.
const (
	DefaultAxisRate = 120
	MaxAxisRate     = 1000
)

// DefaultAxisThreshold is how far a resting axis has to move before it
// counts as moved, unless a profile says otherwise: a hundredth of its
// travel, a little more than one step of a MIDI controller. MaxAxisThreshold
// is the most a profile may ask for.
const (
	DefaultAxisThreshold = 0.01
	MaxAxisThreshold     = 0.5
)

// settings are the values of a [defaults] table that are not part of the
// instrument's starting state.
type settings struct {
	collision engine.CollisionMode
	restAxes  bool
	axisRate  int
	// axisThreshold is described at Profile.AxisThreshold.
	axisThreshold float64
}

func (r *reader) defaults(t *table) (engine.Defaults, settings) {
	d := engine.Defaults{
		Octave:    int(t.integer("octave", 0, -engine.MaxOctave, engine.MaxOctave)),
		Transpose: int(t.integer("transpose", 0, -engine.MaxTranspose, engine.MaxTranspose)),
		// Channels are 1-16 in files and 0-15 in the engine.
		Channel:  uint8(t.integer("channel", 1, 1, 16) - 1),
		Velocity: uint8(t.integer("velocity", DefaultVelocity, 1, 127)),
		Layout:   t.str("layout", ""),
	}
	collision, err := engine.ParseCollisionMode(t.str("collision", engine.CollisionInterrupt.String()))
	if err != nil {
		r.failf(t.where, "collision: %v; use interrupt, no_repeat, retrigger or off", err)
	}
	s := settings{collision: collision}
	switch mode := t.str("axis_on_change", "keep"); mode {
	case "keep":
	case "rest":
		s.restAxes = true
	default:
		r.failf(t.where, "axis_on_change: unknown mode %q; use keep or rest", mode)
	}
	s.axisRate = int(t.integer("axis_rate", DefaultAxisRate, 0, MaxAxisRate))
	s.axisThreshold = t.number("axis_threshold", DefaultAxisThreshold)
	if !(s.axisThreshold >= 0 && s.axisThreshold <= MaxAxisThreshold) {
		r.failf(t.where, "axis_threshold must be from 0 to %v, not %v", MaxAxisThreshold, s.axisThreshold)
		s.axisThreshold = DefaultAxisThreshold
	}
	t.done("octave", "transpose", "channel", "velocity", "layout", "collision", "axis_on_change", "axis_rate", "axis_threshold")
	return d, s
}

// reader collects the problems found while reading a file, so that all of
// them can be reported together.
type reader struct {
	errs []error
}

func (r *reader) failf(where, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if where != "" {
		msg = where + ": " + msg
	}
	r.errs = append(r.errs, errors.New(msg))
}

// table wraps a decoded TOML table with typed accessors that record problems
// instead of returning them.
type table struct {
	r      *reader
	where  string
	values map[string]any
}

// table wraps values and reports keys that are not in allowed. Pass no
// allowed keys to check them later with done.
func (r *reader) table(where string, values map[string]any, allowed ...string) *table {
	t := &table{r, where, values}
	if len(allowed) > 0 {
		t.done(allowed...)
	}
	return t
}

// done reports every key of the table that is not in allowed.
func (t *table) done(allowed ...string) {
	var unknown []string
	for key := range t.values {
		if !contains(allowed, key) {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		t.r.failf(t.where, "unknown setting %q; the settings here are %s", key, strings.Join(allowed, ", "))
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (t *table) has(key string) bool {
	_, ok := t.values[key]
	return ok
}

// sub returns the table stored under key, or an empty table if the key is
// missing or holds something else.
func (t *table) sub(key string) *table {
	where := key
	if t.where != "" {
		where = t.where + ", " + key
	}
	raw, ok := t.values[key]
	if !ok {
		return &table{t.r, where, nil}
	}
	values, ok := raw.(map[string]any)
	if !ok {
		t.r.failf(where, "must be a table, not %s", describe(raw))
	}
	return &table{t.r, where, values}
}

func (t *table) str(key, fallback string) string {
	raw, ok := t.values[key]
	if !ok {
		return fallback
	}
	s, ok := raw.(string)
	if !ok {
		t.r.failf(t.where, "%s must be a string, not %s", key, describe(raw))
		return fallback
	}
	return s
}

func (t *table) integer(key string, fallback, lo, hi int64) int64 {
	raw, ok := t.values[key]
	if !ok {
		return fallback
	}
	n, ok := raw.(int64)
	if !ok {
		t.r.failf(t.where, "%s must be a whole number, not %s", key, describe(raw))
		return fallback
	}
	if n < lo || n > hi {
		t.r.failf(t.where, "%s is %d; it must be from %d to %d", key, n, lo, hi)
		return fallback
	}
	return n
}

func (t *table) number(key string, fallback float64) float64 {
	switch n := t.values[key].(type) {
	case nil:
		return fallback
	case float64:
		return n
	case int64:
		return float64(n)
	default:
		t.r.failf(t.where, "%s must be a number, not %s", key, describe(n))
		return fallback
	}
}

func (t *table) boolean(key string, fallback bool) bool {
	raw, ok := t.values[key]
	if !ok {
		return fallback
	}
	b, ok := raw.(bool)
	if !ok {
		t.r.failf(t.where, "%s must be true or false, not %s", key, describe(raw))
		return fallback
	}
	return b
}

// id reads an optional 16-bit hardware identifier.
func (t *table) id(key string) *uint16 {
	if !t.has(key) {
		return nil
	}
	n := uint16(t.integer(key, 0, 0, 0xFFFF))
	return &n
}

// Select returns the profile that describes the device most closely: the
// one with the highest [Match.Score] among those that select it. When
// several score the same, the one later in the list wins, so that profiles
// added by the user take precedence over built-in ones listed before them.
// It returns nil if no profile selects the device.
func Select(profiles []*Profile, d control.DeviceInfo) *Profile {
	var best *Profile
	bestScore := -1
	for _, p := range profiles {
		if score, ok := p.Match.Score(d); ok && score >= bestScore {
			best, bestScore = p, score
		}
	}
	return best
}
