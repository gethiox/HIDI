package config

import (
	"strings"
	"testing"
)

const layoutsText = `schema = 1
name = "P"

[defaults]
velocity = 100

# The piano, played most.
[[layout]]
name = "Piano"

[layout.bindings]
KEY_A = "C3"   # the first key
KEY_S = "D3"

# Drums.
[[layout]]
name = "Drums"
[layout.bindings]
KEY_A = "C1"

[global]
KEY_F1 = "layout_next"
`

func layoutNames(t *testing.T, text []byte) string {
	t.Helper()
	p, err := Parse(text)
	if err != nil {
		t.Fatalf("not a profile any more: %v\n%s", err, text)
	}
	var names []string
	for _, l := range p.Engine.Layouts {
		names = append(names, l.Name)
	}
	return strings.Join(names, ",")
}

func TestAddLayout(t *testing.T) {
	out, err := AddLayout([]byte(layoutsText), `My "own"`)
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutNames(t, out); got != `Piano,Drums,My "own"` {
		t.Errorf("layouts: %s", got)
	}
	// Behind the last layout and before what follows it, which keeps
	// its blank line.
	text := string(out)
	if !strings.Contains(text, "KEY_A = \"C1\"\n\n[[layout]]\nname = \"My \\\"own\\\"\"\n\n[layout.bindings]\n\n[global]\n") {
		t.Errorf("the new layout is not where it belongs:\n%s", text)
	}
	if _, err := AddLayout(out, "Piano"); err == nil {
		t.Error("a second layout of one name was added")
	}
	if _, err := AddLayout(out, "  "); err == nil {
		t.Error("a layout without a name was added")
	}
	// The new layout takes bindings like any other.
	if _, err := Set(out, Bindings(`My "own"`), "KEY_B", `"E3"`); err != nil {
		t.Errorf("a binding in the new layout: %v", err)
	}

	// A file that ends without a line break, in lines that end in CR LF.
	crlf := strings.TrimSuffix(strings.ReplaceAll(strings.Split(layoutsText, "[global]")[0], "\n", "\r\n"), "\r\n\r\n")
	out, err = AddLayout([]byte(crlf), "New")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(string(out), "\r\n", ""), "\n") || layoutNames(t, out) != "Piano,Drums,New" {
		t.Errorf("CR LF was not kept, or the layouts are wrong:\n%q", out)
	}
}

func TestRemoveLayout(t *testing.T) {
	out, err := RemoveLayout([]byte(layoutsText), "Drums")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutNames(t, out); got != "Piano" {
		t.Errorf("layouts: %s", got)
	}
	// Its note went with it; the rest is as it was.
	if strings.Contains(string(out), "Drums") || !strings.Contains(string(out), "# the first key") || !strings.Contains(string(out), "[global]\nKEY_F1") {
		t.Errorf("after removing:\n%s", out)
	}
	if _, err := RemoveLayout(out, "Piano"); err == nil {
		t.Error("the last layout was removed")
	}
	if _, err := RemoveLayout([]byte(layoutsText), "None"); err == nil {
		t.Error("a layout that is not there was removed")
	}
}

func TestMoveLayout(t *testing.T) {
	up, err := MoveLayout([]byte(layoutsText), "Drums", -1)
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutNames(t, up); got != "Drums,Piano" {
		t.Errorf("layouts: %s", got)
	}
	text := string(up)
	// Every layout took its note and its bindings along, and what stood
	// around them is where it was.
	if !strings.Contains(text, "velocity = 100\n\n# Drums.\n[[layout]]\nname = \"Drums\"\n[layout.bindings]\nKEY_A = \"C1\"\n\n# The piano, played most.\n[[layout]]") ||
		!strings.HasSuffix(text, "KEY_S = \"D3\"\n\n[global]\nKEY_F1 = \"layout_next\"\n") {
		t.Errorf("after moving up:\n%s", text)
	}
	// And back: the file is as before.
	back, err := MoveLayout(up, "Drums", 1)
	if err != nil || string(back) != layoutsText {
		t.Errorf("moved up and down again, %v:\n%s", err, back)
	}
	// At the end of the row nothing happens.
	if same, err := MoveLayout([]byte(layoutsText), "Piano", -1); err != nil || string(same) != layoutsText {
		t.Errorf("the first layout moved up: %v", err)
	}
	if _, err := MoveLayout([]byte(layoutsText), "None", 1); err == nil {
		t.Error("a layout that is not there was moved")
	}
	// The last layout of a file that ends with it, without a line break.
	bare := strings.TrimSuffix(strings.Split(layoutsText, "\n[global]")[0], "\n")
	out, err := MoveLayout([]byte(bare), "Drums", -1)
	if err != nil || layoutNames(t, out) != "Drums,Piano" {
		t.Errorf("a file that ends with the layout: %v\n%s", err, out)
	}
}
