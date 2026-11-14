// Command hidi turns keyboards and game controllers into MIDI instruments.
//
// HIDI 2.0 is under development; this binary currently only reports its
// version. See docs/architecture.md for the planned structure.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gethiox/HIDI/internal/buildinfo"
)

const usage = `HIDI turns keyboards and game controllers into MIDI instruments.

Usage:
  hidi <command>

Commands:
  version   print version information
  help      show this help

HIDI 2.0 is under development and has no other commands yet.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command line and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, buildinfo.Get())
		return 0
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "hidi: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
