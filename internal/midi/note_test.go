package midi

import "testing"

func TestNoteString(t *testing.T) {
	tests := map[Note]string{0: "C-2", 1: "C#-2", 11: "B-2", 12: "C-1", 60: "C3", 69: "A3", 127: "G8"}
	for note, want := range tests {
		if got := note.String(); got != want {
			t.Errorf("Note(%d).String() = %q, want %q", note, got, want)
		}
	}
}

func TestParseNote(t *testing.T) {
	tests := []struct {
		in   string
		want Note
	}{
		{"C-2", 0}, {"c-2", 0}, {"C3", 60}, {"c#3", 61}, {"Db3", 61},
		{"f#-1", 18}, {"Bb2", 58}, {"G8", 127}, {"Cb-1", 11}, {"B#2", 60},
		{"0", 0}, {"60", 60}, {"127", 127},
	}
	for _, tt := range tests {
		got, err := ParseNote(tt.in)
		if err != nil {
			t.Errorf("ParseNote(%q) returned error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseNote(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseNoteErrors(t *testing.T) {
	for _, in := range []string{"", "H3", "C", "C#", "C##3", "3C", "128", "-1", "G#8", "Cb-2", "C3 ", "C9"} {
		if got, err := ParseNote(in); err == nil {
			t.Errorf("ParseNote(%q) = %d, want an error", in, got)
		}
	}
}

func TestParseNoteRoundTrip(t *testing.T) {
	for n := Note(0); n <= MaxNote; n++ {
		got, err := ParseNote(n.String())
		if err != nil || got != n {
			t.Errorf("ParseNote(%q) = %d, %v; want %d", n.String(), got, err, n)
		}
	}
}
