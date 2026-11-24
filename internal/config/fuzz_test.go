package config

import (
	"bytes"
	"io/fs"
	"testing"
)

// seeds are the profiles HIDI comes with, and a few texts that are no
// profiles.
func seeds(f *testing.F) {
	names, _ := fs.Glob(builtinFiles, "builtin/*.toml")
	for _, name := range names {
		data, err := builtinFiles.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	for _, s := range []string{"", "schema = 1", "[[layout]]\nname = \"L\"\n[layout.bindings]\nKEY_A = {", "\"\"\"", "a = [[[[[[", "schema = 1\r\nname = \"x\"\r\n"} {
		f.Add([]byte(s))
	}
}

// FuzzParse: whatever a profile file holds, reading it reports a profile
// or an error and nothing else.
func FuzzParse(f *testing.F) {
	f.Add([]byte("schema = 1\nname = \"G\"\n[generate]\nnotes = \"C1\"\ncontrollers = 16\n[[layout]]\nname = \"L\"\n[layout.bindings]\nBTN_A = \"nothing\"\n"))
	seeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data)
		if err == nil && p == nil {
			t.Fatal("neither a profile nor an error")
		}
		if err != nil {
			return
		}
		// Whatever rule a profile has, what it makes for a device is an
		// instrument.
		if made := p.For(fuzzControls()); made.Validate() != nil {
			t.Fatalf("what the profile makes for a device is no instrument: %v", made.Validate())
		}
	})
}

// FuzzEdit: the functions that change the text of a profile either
// refuse, or return a text that is a profile; and reading a key after
// setting it gives what was set.
func FuzzEdit(f *testing.F) {
	seeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		good := func(what string) func([]byte, error) []byte {
			return func(out []byte, err error) []byte {
				if err != nil {
					return nil
				}
				if _, perr := Parse(out); perr != nil {
					t.Fatalf("%s left a text that is no profile: %v\n%s", what, perr, out)
				}
				return out
			}
		}
		if out := good("AddLayout")(AddLayout(data, "Fuzzed")); out != nil {
			// What was added can be bound in, moved and removed.
			section := Bindings("Fuzzed")
			if set := good("Set")(Set(out, section, "KEY_A", `"C3"`)); set != nil {
				value, found, err := Get(set, section, "KEY_A")
				if err != nil || !found || value != `"C3"` {
					t.Fatalf("after Set, Get gave %q, %v, %v", value, found, err)
				}
				if gone := good("Remove")(Remove(set, section, "KEY_A")); gone != nil {
					if _, found, _ := Get(gone, section, "KEY_A"); found {
						t.Fatal("the key is still there after Remove")
					}
				}
			}
			good("MoveLayout")(MoveLayout(out, "Fuzzed", -1))
			if back := good("RemoveLayout")(RemoveLayout(out, "Fuzzed")); back != nil && bytes.Contains(back, []byte(`name = "Fuzzed"`)) && !bytes.Contains(data, []byte(`name = "Fuzzed"`)) {
				t.Fatal("the layout is still there after RemoveLayout")
			}
		}
		good("SetEnabled")(SetEnabled(data, false))
		good("SetEnabled")(SetEnabled(data, true))
	})
}
