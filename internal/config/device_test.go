package config

import (
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/control"
)

// A profile for one product, made of the general one: named for the
// device, for its vendor and product alone, and otherwise as written.
func TestForDevice(t *testing.T) {
	files, err := BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	stickID := control.HardwareID{Bus: 3, Vendor: 0x044f, Product: 0xb10a, Version: 0x0111}
	own, err := ForDevice(files["gamepad.toml"], `Thrustmaster "T.16000M"`, control.DeviceGamepad, stickID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(own)
	if err != nil {
		t.Fatalf("%v\n%s", err, own)
	}
	if p.Name != `Thrustmaster "T.16000M"` {
		t.Errorf("name: %q", p.Name)
	}
	if got := p.Match.String(); got != "type=gamepad vendor=0x044f product=0xb10a" {
		t.Errorf("it is for %s", got)
	}
	// Everything else is as it was written: the rule, and the notes
	// that explain it.
	general, _ := Parse(files["gamepad.toml"])
	if p.Generate != general.Generate || len(p.Engine.Global) != len(general.Engine.Global) {
		t.Error("the profile for the device does not say what the general one says")
	}
	if !strings.Contains(string(own), "# A button or an axis that has no line") {
		t.Error("the notes of the file were lost")
	}
	// It is chosen for that product over the general profile, and for
	// no other.
	general.Kind, p.Kind = KindFactory, KindUser
	profiles := []*Profile{general, p}
	if got := Select(profiles, control.DeviceInfo{Type: control.DeviceGamepad, Hardware: stickID}); got != p {
		t.Errorf("the device gets %v", got)
	}
	other := stickID
	other.Product++
	if got := Select(profiles, control.DeviceInfo{Type: control.DeviceGamepad, Hardware: other}); got != general {
		t.Errorf("another product gets %v", got)
	}

	// A profile that was for one device by other marks is for the
	// product afterwards; one without a [match] gets one; and one for
	// keyboards can be made of a profile for anything.
	narrow := "schema = 1\nname = \"N\"\r\n[match]\nbus = 0x0005\nversion = 0x0001\nserial = \"abc\"\nvendor = 0x1111\n[[layout]]\nname = \"L\"\n"
	own, err = ForDevice([]byte(narrow), "Pad", control.DeviceGamepad, stickID)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := Parse(own); err != nil || p.Match.String() != "type=gamepad vendor=0x044f product=0xb10a" {
		t.Errorf("made of a narrow profile: %v, %v\n%s", p, err, own)
	}
	bare := "name = \"B\"\nschema = 1\n[[layout]]\nname = \"L\"\n[layout.bindings]\nKEY_A = \"C3\"\n"
	own, err = ForDevice([]byte(bare), "Board", control.DeviceKeyboard, control.HardwareID{Vendor: 0x03f0, Product: 0x147f})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := Parse(own); err != nil || p.Name != "Board" || p.Match.String() != "type=keyboard vendor=0x03f0 product=0x147f" || len(p.Engine.Layouts[0].Bindings) != 1 {
		t.Errorf("made of a profile without [match]: %v, %v\n%s", p, err, own)
	}
	// A device that does not say what it is.
	if _, err := ForDevice([]byte(bare), "X", control.DeviceGamepad, control.HardwareID{}); err == nil {
		t.Error("a profile was made for a device without vendor and product")
	}

	for name, want := range map[string]string{
		"Thrustmaster T.16000M":      "thrustmaster-t-16000m-044f-b10a.toml",
		"  --Ünï code / pad!! ":      "n-code-pad-044f-b10a.toml",
		"":                           "device-044f-b10a.toml",
		strings.Repeat("ab cd ", 20): "ab-cd-ab-cd-ab-cd-ab-cd-ab-cd-ab-cd-ab-cd-ab-cd-044f-b10a.toml",
	} {
		if got := DeviceFile(name, stickID); got != want {
			t.Errorf("DeviceFile(%q) = %q, want %q", name, got, want)
		}
	}
}
