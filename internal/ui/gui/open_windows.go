package gui

import "os/exec"

// openPath shows a directory in the file manager, or opens a file with the
// program Windows uses for it.
func openPath(path string) error {
	cmd := exec.Command("explorer.exe", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // Explorer reports failure also when it worked
	return nil
}
