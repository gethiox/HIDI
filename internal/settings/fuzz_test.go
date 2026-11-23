package settings

import (
	"reflect"
	"testing"
)

// FuzzParse: whatever a settings file holds, reading it reports settings
// or an error; and settings that are written and read again are the
// same.
func FuzzParse(f *testing.F) {
	f.Add(Default().Text())
	for _, s := range []string{"", "schema = 1", "[input]\ntoggle = [\"KEY_A\", ", "[lights.colours]\nwhite = \"#12\"", "[window]\nscale = 1e9", "[devices]\nlist = [1]"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := Parse(data)
		if err != nil {
			return
		}
		again, err := Parse(s.Text())
		if err != nil {
			t.Fatalf("settings that were read cannot be read after writing: %v\n%s", err, s.Text())
		}
		if !reflect.DeepEqual(norm(s), norm(again)) {
			t.Fatalf("written and read again, the settings differ:\n%+v\n%+v", s, again)
		}
	})
}

// norm makes a list without entries the same whether it is there or not.
func norm(s Settings) Settings {
	for _, list := range []*[]string{&s.Devices.List, &s.Output.Connect, &s.Shown.Input, &s.Shown.Output} {
		if len(*list) == 0 {
			*list = nil
		}
	}
	if len(s.Input.Toggle) == 0 {
		s.Input.Toggle = nil
	}
	return s
}
