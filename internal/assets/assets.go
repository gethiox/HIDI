// Package assets holds static files embedded into the binary.
package assets

import _ "embed"

// Pony is the farewell banner printed on exit. It contains one %d verb for
// the number of notes played during the session.
//
//go:embed pony.txt
var Pony string
