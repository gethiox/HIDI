//go:build !linux && !windows

package gui

import "errors"

func openPath(path string) error {
	return errors.New("HIDI cannot open a file manager on this system")
}
