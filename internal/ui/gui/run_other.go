//go:build !((linux || windows) && (amd64 || arm64) && !cgo)

package gui

import (
	"context"
	"errors"
	"runtime"
)

var noWindow = func() string {
	if (runtime.GOOS == "linux" || runtime.GOOS == "windows") && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		return "this build of HIDI has no window: it was built with cgo turned on; build it with CGO_ENABLED=0"
	}
	return "HIDI has no window on this system; it needs 64-bit Linux or Windows"
}()

// Run reports that this build of HIDI has no window. The window is drawn
// with a library that HIDI loads without cgo, on 64-bit Linux and Windows;
// a build with cgo turned on would compile that library from C instead,
// which HIDI does not do.
func Run(ctx context.Context, hidi Controller, opts Options) error {
	return errors.New(noWindow)
}
