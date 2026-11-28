//go:build headless

package gui

import (
	"context"
	"errors"
)

// Run reports that this build of HIDI has no window. The tag headless
// leaves it out on purpose, for machines without a desktop: the library
// the window is drawn with loads libX11 and libffi when the program
// starts, and the program would not start without them.
func Run(ctx context.Context, hidi Controller, opts Options) error {
	return errors.New("this build of HIDI has no window: it is the one for machines without a desktop")
}
