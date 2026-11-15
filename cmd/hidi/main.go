// Command hidi turns keyboards and game controllers into MIDI instruments.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/gethiox/HIDI/internal/backend"
	"github.com/gethiox/HIDI/internal/buildinfo"
	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/instrument"
)

const usage = `HIDI turns keyboards and game controllers into MIDI instruments.

Usage:
  hidi <command> [options]

Commands:
  run       run the instruments until interrupted
  devices   list the connected input devices and the profile each would use
  version   print version information
  help      show this help

Run "hidi <command> -h" for the options of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command line and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "run":
		return cmdRun(ctx, args[1:], stderr)
	case "devices":
		return cmdDevices(args[1:], stdout, stderr)
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

// parseFlags parses the options of a command. It returns false if the
// command should not run, with the exit code to use.
func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer) (ok bool, code int) {
	fs.SetOutput(stderr)
	switch err := fs.Parse(args); {
	case err == flag.ErrHelp:
		return false, 0
	case err != nil:
		return false, 2
	case fs.NArg() > 0:
		fmt.Fprintf(stderr, "hidi %s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		return false, 2
	}
	return true, 0
}

func cmdRun(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "also log every MIDI message and every ignored device")
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	profiles, err := config.Builtin()
	if err != nil {
		log.Error("loading the built-in profiles failed", "error", err)
		return 1
	}
	source, err := backend.Devices(func(what string, err error) {
		log.Warn("input device cannot be used", "device", what, "error", err)
	})
	if err != nil {
		log.Error("watching for input devices failed", "error", err)
		return 1
	}
	defer source.Close()

	log.Info("HIDI started; press Ctrl+C to stop", "version", buildinfo.Get().String())
	manager := instrument.NewManager(instrument.Options{
		Profiles:     profiles,
		OpenEndpoint: backend.OpenEndpoint,
		Logger:       log,
	})
	manager.Run(ctx, source.Devices)
	log.Info("HIDI stopped")
	return 0
}

func cmdDevices(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	profiles, err := config.Builtin()
	if err != nil {
		fmt.Fprintf(stderr, "hidi devices: loading the built-in profiles failed: %v\n", err)
		return 1
	}
	source, err := backend.Devices(func(what string, err error) {
		fmt.Fprintf(stderr, "hidi devices: %s cannot be used: %v\n", what, err)
	})
	if err != nil {
		fmt.Fprintf(stderr, "hidi devices: %v\n", err)
		return 1
	}
	defer source.Close()

	// The devices that are already connected arrive right away. Listing
	// ends once nothing new has arrived for a moment.
	var lines []string
	quiet := time.NewTimer(time.Second)
	defer quiet.Stop()
collect:
	for {
		select {
		case dev, ok := <-source.Devices:
			if !ok {
				break collect
			}
			info := dev.Info()
			dev.Close()
			profile := "-"
			if p := config.Select(profiles, info); p != nil {
				profile = p.Name
			}
			lines = append(lines, fmt.Sprintf("%-10s %s  %-40q profile: %s", info.Type, info.Hardware, info.Name, profile))
			quiet.Reset(500 * time.Millisecond)
		case <-quiet.C:
			break collect
		}
	}

	sort.Strings(lines)
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	if len(lines) == 0 {
		fmt.Fprintln(stderr, "hidi devices: no input devices found")
	}
	return 0
}
