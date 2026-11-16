package gui

import "os/exec"

// openPath shows a directory in the file manager, or opens a file with the
// program the desktop uses for it.
func openPath(path string) error {
	cmd := exec.Command("xdg-open", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
