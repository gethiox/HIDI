package gui

import "testing"

func TestLineEditMoves(t *testing.T) {
	l := newLineEdit("hello world")
	if l.selected() != "hello world" {
		t.Fatalf("a field begins with its text selected: %q", l.selected())
	}
	// An arrow ends the selection at its side.
	l.left(false, false)
	if l.cursor != 0 || l.selected() != "" {
		t.Errorf("Left on a selection: cursor %d, selected %q", l.cursor, l.selected())
	}
	l.right(false, false, false)
	l.right(false, true, false) // Shift+Right
	if l.selected() != "e" {
		t.Errorf("Shift+Right: %q", l.selected())
	}
	l.place(len(l.text), true) // Shift+End
	if l.selected() != "ello world" {
		t.Errorf("Shift+End: %q", l.selected())
	}
	l.place(0, false) // Home
	if l.cursor != 0 || l.selected() != "" {
		t.Errorf("Home: %d %q", l.cursor, l.selected())
	}
	l.selectAll()
	if l.selected() != "hello world" {
		t.Errorf("Ctrl+A: %q", l.selected())
	}
}

// Ctrl+Left goes to the beginning of a word everywhere; Ctrl+Right to the
// beginning of the next word on Windows and to the end of the word
// elsewhere.
func TestLineEditWords(t *testing.T) {
	text := []rune("  KEY_A+ left  2.5")
	for _, tt := range []struct {
		from, back, winRight, gtkRight int
	}{
		{0, 0, 2, 7},     // before the spaces
		{2, 0, 7, 7},     // at KEY_A: to "+", which is a word of signs
		{5, 2, 7, 7},     // in KEY_A
		{7, 2, 9, 8},     // at "+"
		{8, 7, 9, 13},    // after "+"
		{13, 9, 15, 16},  // after "left": the "2" of "2.5" is a word
		{15, 9, 16, 16},  // at "2"
		{18, 17, 18, 18}, // at the end
	} {
		if got := wordStart(text, tt.from); got != tt.back {
			t.Errorf("Ctrl+Left from %d: %d, want %d", tt.from, got, tt.back)
		}
		if got := wordAfter(text, tt.from, true); got != tt.winRight {
			t.Errorf("Ctrl+Right on Windows from %d: %d, want %d", tt.from, got, tt.winRight)
		}
		if got := wordAfter(text, tt.from, false); got != tt.gtkRight {
			t.Errorf("Ctrl+Right elsewhere from %d: %d, want %d", tt.from, got, tt.gtkRight)
		}
	}
}

func TestLineEditTyping(t *testing.T) {
	l := newLineEdit("abc")
	l.insert("x", fieldRules{}) // replaces the selection
	if l.String() != "x" || l.cursor != 1 {
		t.Errorf("typing over the selection: %q at %d", l.String(), l.cursor)
	}
	l.insert("yz", fieldRules{most: 2})
	if l.String() != "xy" {
		t.Errorf("at most two: %q", l.String())
	}
	l.place(0, false)
	l.erase(true, false, false) // Backspace at the start
	l.erase(false, false, false)
	if l.String() != "y" {
		t.Errorf("Delete: %q", l.String())
	}
	l = newLineEdit("one two three")
	l.place(len(l.text), false)
	l.erase(true, true, false) // Ctrl+Backspace
	if l.String() != "one two " {
		t.Errorf("Ctrl+Backspace: %q", l.String())
	}
	l.place(0, false)
	l.right(false, true, false)
	l.right(false, true, false)
	l.right(false, true, false)
	if got := l.cut(); got != "one" || l.String() != " two " {
		t.Errorf("Ctrl+X: cut %q, left %q", got, l.String())
	}
	l.insert("\tfour\n", fieldRules{}) // what cannot be shown is left out
	if l.String() != "four two " {
		t.Errorf("pasted: %q", l.String())
	}
}

func TestLineEditNumbers(t *testing.T) {
	whole := fieldRules{kind: wholeNumber}
	l := newLineEdit("")
	l.insert("-12.5a3", whole)
	if l.String() != "1253" {
		t.Errorf("a whole number: %q", l.String())
	}
	signed := fieldRules{kind: wholeNumber, signed: true}
	l = newLineEdit("")
	l.insert("-1-2", signed)
	if l.String() != "-12" {
		t.Errorf("a signed whole number: %q", l.String())
	}
	l.place(0, false)
	l.insert("5", signed) // nothing in front of the sign
	if l.String() != "-12" {
		t.Errorf("a digit before the sign: %q", l.String())
	}
	decimal := fieldRules{kind: decimalNumber}
	l = newLineEdit("")
	l.insert("2,5.7", decimal)
	if l.String() != "2,57" {
		t.Errorf("a decimal number takes one separator: %q", l.String())
	}
	l = newLineEdit("12")
	l.insert("x", whole) // a key the field does not take leaves the selection
	if l.String() != "12" {
		t.Errorf("a letter in a number field: %q", l.String())
	}
}
