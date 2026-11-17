package rawinput

import (
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

func TestKeyCode(t *testing.T) {
	for _, tt := range []struct {
		name            string
		makeCode, flags uint16
		want            string // the Linux name, or "" for no key
	}{
		{"escape", 0x01, 0, "KEY_ESC"},
		{"A", 0x1E, 0, "KEY_A"},
		{"Z", 0x2C, 0, "KEY_Z"},
		{"left shift", 0x2A, 0, "KEY_LEFTSHIFT"},
		{"left ctrl", 0x1D, 0, "KEY_LEFTCTRL"},
		{"left alt", 0x38, 0, "KEY_LEFTALT"},
		{"F1", 0x3B, 0, "KEY_F1"},
		{"F12", 0x58, 0, "KEY_F12"},
		{"keypad enter", 0x1C, flagE0, "KEY_KPENTER"},
		{"right ctrl", 0x1D, flagE0, "KEY_RIGHTCTRL"},
		{"right alt", 0x38, flagE0, "KEY_RIGHTALT"},
		{"up", 0x48, flagE0, "KEY_UP"},
		{"delete", 0x53, flagE0, "KEY_DELETE"},
		{"left Windows key", 0x5B, flagE0, "KEY_LEFTMETA"},
		{"volume up", 0x30, flagE0, "KEY_VOLUMEUP"},
		{"play", 0x22, flagE0, "KEY_PLAYPAUSE"},
		{"release keeps the code", 0x1E, flagBreak, "KEY_A"},
		{"pause", 0x1D, flagE1, "KEY_PAUSE"},
		{"num lock", 0x45, 0, "KEY_NUMLOCK"},
		{"F13", 0x64, 0, "KEY_F13"},
		{"made-up shift before an arrow", 0x2A, flagE0, ""},
		{"made-up right shift", 0x36, flagE0 | flagBreak, ""},
		{"overrun", 0xFF, 0, ""},
		{"nothing", 0, 0, ""},
		{"unknown extended code", 0x01, flagE0, ""},
		{"unknown plain code", 0x7F, 0, ""},
	} {
		code, ok := keyCode(tt.makeCode, tt.flags)
		got := ""
		if ok {
			got = control.Key(code).String()
		}
		if got != tt.want {
			t.Errorf("%s: keyCode(%#x, %#x) = %q, want %q", tt.name, tt.makeCode, tt.flags, got, tt.want)
		}
	}
}

// Every code in the tables must be a key HIDI can name, so that a typing
// mistake in a number shows up here.
func TestKeyTablesNameRealKeys(t *testing.T) {
	for _, table := range []map[uint16]uint16{extended, plainExtra} {
		for scan, code := range table {
			name := control.Key(code).String()
			if _, err := control.ParseCode(name); err != nil || len(name) < 5 || name[:4] != "KEY_" || name[:6] == "KEY_0x" {
				t.Errorf("scan code %#x maps to %d, which is %q", scan, code, name)
			}
		}
	}
}

func TestParsePath(t *testing.T) {
	keys := parsePath(`\\?\HID#VID_046D&PID_C31C&MI_00#7&2a1b3c4d&0&0000#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`)
	media := parsePath(`\\?\HID#VID_046D&PID_C31C&MI_01&Col01#7&2a1b3c4d&0&0001#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`)
	other := parsePath(`\\?\HID#VID_046D&PID_C31C&MI_00#7&99999999&0&0000#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`)

	if keys.vendor != 0x046D || keys.product != 0xC31C {
		t.Errorf("identifiers %04x:%04x, want 046d:c31c", keys.vendor, keys.product)
	}
	if keys.group != media.group {
		t.Errorf("two interfaces of one keyboard are in different groups:\n %s\n %s", keys.group, media.group)
	}
	if keys.group == other.group {
		t.Error("two keyboards of the same model are in one group")
	}

	lower := parsePath(`\\?\hid#vid_046d&pid_c31c&mi_00#7&2a1b3c4d&0&0000#{guid}`)
	if lower.vendor != 0x046D || lower.group != keys.group {
		t.Errorf("a path in lower case parsed to %+v", lower)
	}

	// Paths of another form keep working, each as a device of its own.
	for _, path := range []string{"", `\\?\ACPI#PNP0303#4&1234&0#{guid}`, "nonsense"} {
		id := parsePath(path)
		if id.vendor != 0 || id.product != 0 || (path != "" && id.group == "") {
			t.Errorf("parsePath(%q) = %+v", path, id)
		}
	}
	if a, b := parsePath(`\\?\ACPI#PNP0303#4&1234&0#{guid}`), parsePath(`\\?\ACPI#PNP0303#4&9999&0#{guid}`); a.group == b.group {
		t.Error("two built-in keyboards share a group")
	}
}

func TestQueue(t *testing.T) {
	q := newQueue()
	// Far more than the channel holds, without a reader: put must not wait.
	done := make(chan struct{})
	go func() {
		for i := range 10000 {
			q.put(control.Event{Code: control.Key(30), Value: float64(i)})
		}
		q.close()
		q.put(control.Event{Value: -1}) // dropped
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("put blocked without a reader")
	}
	n := 0
	for ev := range q.out {
		if ev.Value != float64(n) {
			t.Fatalf("event %d has value %v: order lost", n, ev.Value)
		}
		n++
	}
	if n != 10000 {
		t.Errorf("%d events delivered, want 10000", n)
	}
}

func TestQueueCloseWhileEmpty(t *testing.T) {
	q := newQueue()
	q.close()
	select {
	case _, ok := <-q.out:
		if ok {
			t.Error("an event came out of an empty queue")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the output was not closed")
	}
}

func TestGate(t *testing.T) {
	const enter, a, b = 0x0D, 0x41, 0x42
	g := &gate{passed: map[uint32]bool{}}
	step := func(what string, vk uint32, up, want bool) {
		t.Helper()
		if got := g.swallow(vk, up); got != want {
			t.Errorf("%s: swallowed %v, want %v", what, got, want)
		}
	}

	// Nothing is grabbed: keys pass.
	step("a press with no grab", a, false, false)
	step("its release", a, true, false)

	// Enter is down when the grab begins; its release must still pass.
	step("enter pressed before the grab", enter, false, false)
	g.grabs.Add(1)
	step("a repeat of enter during the grab", enter, false, false)
	step("a new key during the grab", a, false, true)
	step("its repeat", a, false, true)
	step("the release of enter", enter, true, false)
	step("the release of the new key", a, true, true)
	step("enter again, now grabbed", enter, false, true)
	step("and its release", enter, true, true)

	// A key that is down when the grab ends stays swallowed to its release.
	step("b pressed while grabbed", b, false, true)
	g.grabs.Add(-1)
	step("b repeating after the grab", b, false, true)
	step("b released after the grab", b, true, true)
	step("b pressed again", b, false, false)
	step("b released", b, true, false)

	// A release nobody saw the press of follows the present state.
	step("an unknown release without a grab", a, true, false)
	g.grabs.Add(1)
	step("an unknown release during a grab", a, true, true)
}

func TestInstanceID(t *testing.T) {
	for path, want := range map[string]string{
		`\\?\HID#VID_046D&PID_C31C&MI_00#7&2a1b3c4d&0&0000#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`:       `HID\VID_046D&PID_C31C&MI_00\7&2a1b3c4d&0&0000`,
		`\\?\HID#VID_03F0&PID_058F&MI_01&Col02#9&1f2e3d4c&0&0001#{4d1e55b2-f16f-11cf-88cb-001111000030}`: `HID\VID_03F0&PID_058F&MI_01&Col02\9&1f2e3d4c&0&0001`,
		`\\?\ACPI#PNP0303#4&1234&0#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`:                               `ACPI\PNP0303\4&1234&0`,
		`not a path`: "",
		``:           "",
	} {
		if got := instanceID(path); got != want {
			t.Errorf("instanceID(%q) = %q, want %q", path, got, want)
		}
	}
}

// A hidden key is swallowed without any grab; other keys pass.
func TestGateHidesKeys(t *testing.T) {
	var g gate
	hidden := map[uint16]bool{58: true} // KEY_CAPSLOCK
	g.hidden.Store(&hidden)
	g.hides.Add(1)

	const vkCaps, vkA = 0x14, 0x41
	if !g.swallowKey(vkCaps, 58, true, false) || !g.swallowKey(vkCaps, 58, true, true) {
		t.Error("the hidden key got through")
	}
	if g.swallowKey(vkA, 30, true, false) || g.swallowKey(vkA, 30, true, true) {
		t.Error("another key was swallowed")
	}
	// A key whose code is not known is not hidden.
	if g.swallowKey(vkCaps, 0, false, false) {
		t.Error("a key without a code was swallowed")
	}
	g.swallowKey(vkCaps, 0, false, true)

	// Pressed while hidden, it stays swallowed to its release.
	g.swallowKey(vkCaps, 58, true, false)
	g.hides.Add(-1)
	if !g.swallowKey(vkCaps, 58, true, true) {
		t.Error("the release of a swallowed press got through")
	}
	if g.swallowKey(vkCaps, 58, true, false) {
		t.Error("still hidden after hiding ended")
	}
}

// Every key a scan code is known for comes back from it.
func TestScanCodeRoundTrip(t *testing.T) {
	for _, code := range []uint16{1, 30, 58, 88, 97, 100, 103, 111, 125, 183} {
		scan, e0, ok := scanCode(code)
		if !ok {
			t.Errorf("no scan code for key %d", code)
			continue
		}
		var flags uint16
		if e0 {
			flags = flagE0
		}
		if back, ok := keyCode(scan, flags); !ok || back != code {
			t.Errorf("key %d -> scan %#x (E0 %v) -> key %d", code, scan, e0, back)
		}
	}
	if _, _, ok := scanCode(119); ok {
		t.Error("Pause has no single scan code")
	}
}

func TestClaims(t *testing.T) {
	var c claims[string]
	takes := map[string]bool{}
	ask := func(code uint16, pressed bool) string {
		d, ok := c.owner(code, pressed, func(d string) bool { return takes[d] })
		if !ok {
			return ""
		}
		return d
	}
	if got := ask(30, true); got != "" {
		t.Fatalf("a key without any keyboard went to %q", got)
	}
	c.add("mouse")
	c.add("keyboard")
	takes["mouse"], takes["keyboard"] = true, true
	// Nothing was seen typing: the first keyboard gets the key, and its
	// release.
	if got := ask(30, true); got != "mouse" {
		t.Fatalf("the first key went to %q", got)
	}
	c.saw("keyboard")
	if got := ask(30, true); got != "mouse" {
		t.Fatalf("the repeat of a held key went to %q", got)
	}
	if got := ask(30, false); got != "mouse" {
		t.Fatalf("the release went to %q", got)
	}
	// From now on the keyboard that typed gets them.
	if got := ask(30, true); got != "keyboard" {
		t.Fatalf("a key after typing went to %q", got)
	}
	// A keyboard that passes its keys on is not given swallowed ones while
	// another keeps them.
	takes["keyboard"] = false
	if got := ask(31, true); got != "mouse" {
		t.Fatalf("a key went to %q although it keeps none", got)
	}
	if got := ask(30, false); got != "keyboard" {
		t.Fatalf("the release of a key went to %q, not to who got the press", got)
	}
	c.remove("mouse")
	if got := ask(31, false); got != "keyboard" {
		t.Fatalf("the release of a key of an unplugged keyboard went to %q", got)
	}
	c.remove("keyboard")
	if got := ask(32, true); got != "" {
		t.Fatalf("a key went to %q after every keyboard was unplugged", got)
	}
}
