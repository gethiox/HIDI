//go:build (amd64 && !cgo) || (arm64 && !cgo)

package gui

import (
	"os/exec"
	"strings"
	"syscall"
)

// openPath shows a directory in the file manager, or opens a file or a
// web page with the program Windows uses for it.
func openPath(path string) error {
	cmd := exec.Command("explorer.exe", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // Explorer reports failure also when it worked
	return nil
}

// showInFolder opens the directory of a file in Explorer with the file
// chosen.
func showInFolder(path string) error {
	cmd := exec.Command("explorer.exe")
	// Explorer reads its command line in its own way: the path is quoted,
	// the switch before it is not. A path cannot hold a quote.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + strings.ReplaceAll(path, `"`, "") + `"`}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
