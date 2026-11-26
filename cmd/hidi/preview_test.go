package main

import (
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/config"
)

func parsed(t *testing.T, source, text string) *config.Profile {
	t.Helper()
	p, err := config.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	p.Source = source
	return p
}

const (
	profileA  = "schema = 1\nname = \"A\"\n[[layout]]\nname = \"L\"\n[layout.bindings]\nKEY_A = \"C3\"\n"
	profileA2 = "schema = 1\nname = \"A tried\"\n[[layout]]\nname = \"L\"\n[layout.bindings]\nKEY_A = \"D3\"\n"
	profileB  = "schema = 1\nname = \"B\"\n[[layout]]\nname = \"L\"\n[layout.bindings]\nKEY_B = \"E3\"\n"
)

// A profile that is tried stands in for its file until the file changes.
func TestPreviews(t *testing.T) {
	a, b := parsed(t, "/p/a.toml", profileA), parsed(t, "/p/b.toml", profileB)
	p := &previews{profiles: []*config.Profile{a, b}}
	names := func(profiles []*config.Profile) string {
		var out []string
		for _, profile := range profiles {
			out = append(out, profile.Name)
		}
		return strings.Join(out, ",")
	}

	now, err := p.set("/p/a.toml", profileA2)
	if err != nil || names(now) != "A tried,B" || now[0].Source != "/p/a.toml" {
		t.Fatalf("tried: %s, %v", names(now), err)
	}
	if _, err := p.set("/p/a.toml", "name = "); err == nil {
		t.Error("text that is no profile was taken")
	}
	if _, err := p.set("/p/other.toml", profileA2); err == nil {
		t.Error("a file that is no profile in use was taken")
	}
	// The same files read again: the stand-in stays.
	if got := names(p.loaded([]*config.Profile{a, b})); got != "A tried,B" {
		t.Errorf("after loading the same files: %s", got)
	}
	// Another file changed: still there. Its own file changed: gone.
	if got := names(p.loaded([]*config.Profile{a, parsed(t, "/p/b.toml", profileA2)})); got != "A tried,A tried" {
		t.Errorf("after another file changed: %s", got)
	}
	saved := parsed(t, "/p/a.toml", profileA2)
	if got := p.loaded([]*config.Profile{saved, b}); got[0] != saved {
		t.Errorf("after its file changed the stand-in is still used: %s", names(got))
	}
	// Ended by hand.
	p.set("/p/a.toml", profileA)
	if now, _ := p.set("/p/a.toml", ""); now[0] != saved {
		t.Errorf("after ending: %s", names(now))
	}
}

// The profiles to use are handed on from within the lock, by a try and
// by a load alike, so the one worked out last is the one in use.
func TestPreviewsHandOverInOrder(t *testing.T) {
	a, b := parsed(t, "/p/a.toml", profileA), parsed(t, "/p/b.toml", profileB)
	var used [][]*config.Profile
	p := &previews{profiles: []*config.Profile{a, b}}
	p.use = func(profiles []*config.Profile) { used = append(used, profiles) }
	now, _ := p.set("/p/a.toml", profileA2)
	later := p.loaded([]*config.Profile{a, b})
	if len(used) != 2 || &used[0][0] != &now[0] || &used[1][0] != &later[0] {
		t.Errorf("handed over %d times, not what was returned", len(used))
	}
}
