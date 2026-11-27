package gui

import (
	"strings"
	"unicode"
)

// The editing of one line of text, apart from its drawing so that it is
// tested without a window: where the cursor is, what is selected, what
// may be typed, and the moves the keys of every text field make.

// fieldKind says what a text field takes.
type fieldKind uint8

const (
	// anyText takes every character that can be printed.
	anyText fieldKind = iota
	// wholeNumber takes digits, and a minus sign in front where the
	// field is signed.
	wholeNumber
	// decimalNumber takes digits and one separator of the fraction, a
	// point or a comma, and a minus sign in front where it is signed.
	decimalNumber
)

// fieldRules is what a field lets be typed into it.
type fieldRules struct {
	kind fieldKind
	// signed lets a number be negative.
	signed bool
	// most is the most characters the field holds; 0 for no limit.
	most int
	// accept, if set, says further which characters can be typed.
	accept func(rune) bool
}

// lineEdit is a line of text being edited. The selection runs between
// anchor and cursor, in characters; with both at one place, nothing is
// selected and the cursor stands there.
type lineEdit struct {
	text           []rune
	cursor, anchor int
}

// newLineEdit begins the editing of text, with all of it selected, so
// that what is typed first replaces it and an arrow key keeps it.
func newLineEdit(text string) lineEdit {
	r := []rune(text)
	return lineEdit{text: r, cursor: len(r), anchor: 0}
}

func (l *lineEdit) String() string { return string(l.text) }

// selection returns where the selection begins and ends.
func (l *lineEdit) selection() (from, to int) {
	return min(l.cursor, l.anchor), max(l.cursor, l.anchor)
}

// selected returns the selected text.
func (l *lineEdit) selected() string {
	from, to := l.selection()
	return string(l.text[from:to])
}

// place puts the cursor at i; with extend, the selection grows or shrinks
// to it, and otherwise it ends.
func (l *lineEdit) place(i int, extend bool) {
	l.cursor = min(max(i, 0), len(l.text))
	if !extend {
		l.anchor = l.cursor
	}
}

// selectAll selects the whole text.
func (l *lineEdit) selectAll() { l.anchor, l.cursor = 0, len(l.text) }

// left and right move the cursor by a character, or by a word. Without
// extend, a selection ends at its side the move goes to, as in every text
// field, instead of the cursor moving from where it is.
func (l *lineEdit) left(word, extend bool) {
	if from, to := l.selection(); !extend && !word && from != to {
		l.place(from, false)
		return
	}
	to := l.cursor - 1
	if word {
		to = wordStart(l.text, l.cursor)
	}
	l.place(to, extend)
}

func (l *lineEdit) right(word, extend, windows bool) {
	if from, to := l.selection(); !extend && !word && from != to {
		l.place(to, false)
		return
	}
	to := l.cursor + 1
	if word {
		to = wordAfter(l.text, l.cursor, windows)
	}
	l.place(to, extend)
}

// insert puts what is typed or pasted in place of the selection, as far
// as the rules let it: characters the field does not take are left out,
// and the text stops at the most it may hold.
func (l *lineEdit) insert(s string, rules fieldRules) bool {
	from, to := l.selection()
	rest := append(append([]rune{}, l.text[:from]...), l.text[to:]...)
	var add []rune
	for _, c := range s {
		if rules.most > 0 && len(rest)+len(add) >= rules.most {
			break
		}
		now := append(append(append([]rune{}, rest[:from]...), add...), rest[from:]...)
		if rules.takes(c, now, from+len(add)) {
			add = append(add, c)
		}
	}
	if len(add) == 0 {
		return false // a key the field does not take leaves it as it is
	}
	l.text = append(append(append([]rune{}, rest[:from]...), add...), rest[from:]...)
	l.place(from+len(add), false)
	return true
}

// takes reports whether c can go into text at place at.
func (r fieldRules) takes(c rune, text []rune, at int) bool {
	if c < 0x20 || c == 0x7f || !unicode.IsPrint(c) && c != ' ' {
		return false
	}
	if r.accept != nil && !r.accept(c) {
		return false
	}
	whole := text
	switch r.kind {
	case anyText:
		return true
	case decimalNumber:
		if c == '.' || c == ',' {
			return !strings.ContainsAny(string(whole), ".,")
		}
	}
	if c == '-' {
		return r.signed && at == 0 && !strings.ContainsRune(string(whole), '-')
	}
	// Nothing goes in front of the sign.
	if at == 0 && len(whole) > 0 && whole[0] == '-' {
		return false
	}
	return c >= '0' && c <= '9'
}

// erase removes the selection, or else what lies before the cursor
// (back) or after it, a character or a word.
func (l *lineEdit) erase(back, word, windows bool) bool {
	if from, to := l.selection(); from != to {
		l.text = append(l.text[:from:from], l.text[to:]...)
		l.place(from, false)
		return true
	}
	from, to := l.cursor, l.cursor
	switch {
	case back && word:
		from = wordStart(l.text, l.cursor)
	case back:
		from = l.cursor - 1
	case word:
		to = wordAfter(l.text, l.cursor, windows)
	default:
		to = l.cursor + 1
	}
	from, to = max(from, 0), min(to, len(l.text))
	if from == to {
		return false
	}
	l.text = append(l.text[:from:from], l.text[to:]...)
	l.place(from, false)
	return true
}

// cut takes the selection out and returns it.
func (l *lineEdit) cut() string {
	s := l.selected()
	if s != "" {
		l.erase(true, false, false)
	}
	return s
}

// The classes of characters a word is made of: a run of letters and
// digits is one word, a run of other signs another, and spaces stand
// between them.
func class(c rune) int {
	switch {
	case unicode.IsSpace(c):
		return 0
	case unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_':
		return 1
	}
	return 2
}

// wordStart is where Ctrl+Left goes, on every system: back over spaces,
// then to the beginning of the word before them.
func wordStart(text []rune, i int) int {
	for i > 0 && class(text[i-1]) == 0 {
		i--
	}
	if i == 0 {
		return 0
	}
	c := class(text[i-1])
	for i > 0 && class(text[i-1]) == c {
		i--
	}
	return i
}

// wordAfter is where Ctrl+Right goes. On Windows that is the beginning of
// the next word: over the rest of this one and the spaces after it.
// Elsewhere, as in the fields of GTK and Qt, it is the end of the word:
// over spaces, then to the end of the word after them.
func wordAfter(text []rune, i int, windows bool) int {
	n := len(text)
	if windows {
		if i < n && class(text[i]) != 0 {
			c := class(text[i])
			for i < n && class(text[i]) == c {
				i++
			}
		}
		for i < n && class(text[i]) == 0 {
			i++
		}
		return i
	}
	for i < n && class(text[i]) == 0 {
		i++
	}
	if i == n {
		return n
	}
	c := class(text[i])
	for i < n && class(text[i]) == c {
		i++
	}
	return i
}
