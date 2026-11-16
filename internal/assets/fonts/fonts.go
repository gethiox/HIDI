// Package fonts holds the fonts of the window, embedded into the binary:
// DejaVu Sans, its bold cut, and DejaVu Sans Mono. Their licence is in
// LICENSE-DejaVu.txt beside this file.
package fonts

import _ "embed"

var (
	//go:embed DejaVuSans.ttf
	Sans []byte
	//go:embed DejaVuSans-Bold.ttf
	Bold []byte
	//go:embed DejaVuSansMono.ttf
	Mono []byte
)
