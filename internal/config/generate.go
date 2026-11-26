package config

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/midi"
)

// Generate is the rule by which a profile gives a part to the controls it
// has no line for. With it one profile fits a pad of ten buttons and a
// stick of forty: what the profile names does what the profile says, and
// everything else the device has plays by its number.
//
// The numbers are those of the device's own list of controls (see
// [Number]): the buttons of its main part counted from 0 in the order of
// their codes, and the axes likewise. They depend on nothing but what the
// device has, so the same device gets the same instrument at every start.
type Generate struct {
	// Notes says that buttons play notes: the button numbered n plays
	// FirstNote + n. The directions of the hats follow the last button,
	// two for every axis of a hat.
	Notes     bool
	FirstNote midi.Note
	// Controllers says that axes send controllers: the axis numbered n
	// sends the n-th controller from FirstController on, not counting
	// those of [ReservedController].
	Controllers     bool
	FirstController uint8
}

// Any reports whether the rule gives anything a part.
func (g Generate) Any() bool { return g.Notes || g.Controllers }

// ReservedController reports whether a controller number means something
// to an instrument that an axis moved by chance must not say: the choice
// of a bank, data entry, the fine halves of the first 32 controllers, the
// pedals, the numbers of parameters, and the channel mode messages.
func ReservedController(n uint8) bool {
	switch {
	case n == 0, n == 6:
		return true
	case n >= 32 && n <= 69:
		return true
	case n >= 96 && n <= 101:
		return true
	}
	return n >= 120
}

// controller returns the n-th controller from first on that is not
// reserved, and false when the controllers run out before it.
func controller(first uint8, n int) (uint8, bool) {
	for c := int(first); c <= 127; c++ {
		if ReservedController(uint8(c)) {
			continue
		}
		if n == 0 {
			return uint8(c), true
		}
		n--
	}
	return 0, false
}

// Number puts the controls of the main part of a device in the order they
// are numbered in: the buttons by their codes, and the axes by theirs.
// The place of a control in its list is its number. A control listed
// twice counts once.
func Number(controls []control.Input) (buttons, axes []control.Input) {
	for _, c := range controls {
		if c.Part != "" {
			continue
		}
		switch c.Code.Kind {
		case control.KindKey:
			buttons = append(buttons, c)
		case control.KindAxis:
			axes = append(axes, c)
		}
	}
	for _, list := range []*[]control.Input{&buttons, &axes} {
		slices.SortStableFunc(*list, func(a, b control.Input) int { return cmp.Compare(a.Code.Num, b.Code.Num) })
		*list = slices.CompactFunc(*list, func(a, b control.Input) bool { return a.Code == b.Code })
	}
	return buttons, axes
}

// Generated is one binding the rule of a profile makes for a device.
type Generated struct {
	engine.Binding
	// Key and Value are the line that says the same in a profile.
	Key, Value string
}

// Generated returns the bindings the profile's rule makes for a device
// with these controls, one for every control the rule covers, whether or
// not the profile has a line for it; [Profile.For] leaves out those it
// has. A device that lists no controls gets none.
func (p *Profile) Generated(controls []control.Input) []Generated {
	if !p.Generate.Any() {
		return nil
	}
	g := p.Generate
	buttons, axes := Number(controls)
	var out []Generated
	// What the rule binds has the dead zone of the profile, as far as it
	// is an axis.
	deadzone := func(code control.Code) float64 {
		if code.Kind == control.KindAxis {
			return p.Deadzone
		}
		return 0
	}
	note := func(code control.Code, half engine.Range, n int) {
		if !g.Notes || int(g.FirstNote)+n > int(midi.MaxNote) {
			return
		}
		pitch := g.FirstNote + midi.Note(n)
		source := engine.Source{Code: code, Range: half, Deadzone: deadzone(code)}
		out = append(out, Generated{
			Binding: engine.Binding{Source: source, Action: engine.Note{Note: pitch}},
			Key:     SourceKey(source), Value: strconv.Quote(pitch.String()),
		})
	}
	for n, b := range buttons {
		note(b.Code, engine.RangeWhole, n)
	}
	hats := 0
	for n, a := range axes {
		if a.Hat {
			// A hat only ever rests in the middle or at an end: each of
			// its directions is a button.
			note(a.Code, engine.RangeLower, len(buttons)+2*hats)
			note(a.Code, engine.RangeUpper, len(buttons)+2*hats+1)
			hats++
			continue
		}
		if !g.Controllers {
			continue
		}
		if cc, ok := controller(g.FirstController, n); ok {
			out = append(out, Generated{
				Binding: engine.Binding{Source: engine.Source{Code: a.Code, Deadzone: p.Deadzone}, Action: engine.ControlChange{Controller: cc}},
				Key:     a.Code.String(), Value: fmt.Sprintf("{ cc = %d }", cc),
			})
		}
	}
	return out
}

// For returns the instrument the profile describes for a device with these
// controls: what the profile says, and in every layout, for every control
// that has no line there nor among those that apply in every layout, what
// the profile's rule makes of it. Without a rule it is [Profile.Engine].
func (p *Profile) For(controls []control.Input) engine.Profile {
	generated := p.Generated(controls)
	if len(generated) == 0 {
		return p.Engine
	}
	out := p.Engine
	always := named(nil, p.Engine.Global)
	out.Layouts = slices.Clone(p.Engine.Layouts)
	for i, l := range out.Layouts {
		has := named(always, l.Bindings)
		bindings := slices.Clone(l.Bindings)
		for _, g := range generated {
			if !has[g.Source.Code] {
				bindings = append(bindings, g.Binding)
			}
		}
		out.Layouts[i].Bindings = bindings
	}
	return out
}

// named returns the controls of the main part that bindings have a line
// for, in whatever way, added to those of base.
func named(base map[control.Code]bool, bindings []engine.Binding) map[control.Code]bool {
	out := make(map[control.Code]bool, len(base)+len(bindings))
	for code := range base {
		out[code] = true
	}
	for _, b := range bindings {
		if b.Source.Part == "" {
			out[b.Source.Code] = true
		}
	}
	return out
}

// generate reads the [generate] table.
func (r *reader) generate(t *table) Generate {
	var g Generate
	if t.has("notes") {
		note, err := midi.ParseNote(fmt.Sprint(t.values["notes"]))
		if err != nil {
			r.failf(t.where, "notes: %v", err)
		}
		g.Notes, g.FirstNote = true, note
	}
	if t.has("controllers") {
		g.Controllers, g.FirstController = true, uint8(t.integer("controllers", 0, 0, 127))
	}
	t.done("notes", "controllers")
	return g
}
