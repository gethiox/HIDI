//go:build (amd64 && !cgo) || (arm64 && !cgo)

package gui

import (
	"net/url"
	"os/exec"
	"path/filepath"

	"github.com/godbus/dbus/v5"
)

// openPath shows a directory in the file manager, or opens a file or a
// web page with the program the desktop uses for it.
func openPath(path string) error {
	cmd := exec.Command("xdg-open", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// showInFolder opens the directory of a file in the file manager with
// the file chosen, where the desktop has a file manager that can be
// asked to; otherwise the directory is opened.
func showInFolder(path string) error {
	if conn, err := dbus.SessionBus(); err == nil {
		uri := (&url.URL{Scheme: "file", Path: path}).String()
		manager := conn.Object("org.freedesktop.FileManager1", "/org/freedesktop/FileManager1")
		if manager.Call("org.freedesktop.FileManager1.ShowItems", 0, []string{uri}, "").Err == nil {
			return nil
		}
	}
	return openPath(filepath.Dir(path))
}
