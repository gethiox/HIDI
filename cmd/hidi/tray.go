package main

import (
	"context"
	"fmt"
	"github.com/spf13/pflag"
	"io"
	"log/slog"
	"os"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/logging"
	"github.com/gethiox/HIDI/internal/ui/tray"
)

// cmdTray shows the icon in the notification area until interrupted or
// until the user removes it. It is a client of the running HIDI and does
// not start or stop it.
func cmdTray(ctx context.Context, args []string, stderr io.Writer) int {
	fs := pflag.NewFlagSet("tray", pflag.ContinueOnError)
	socket := socketFlag(fs)
	noColour := fs.Bool("no-color", false, "write the log without colours")
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	file, isFile := stderr.(*os.File)
	log := logging.New(logging.Options{
		Output:    stderr,
		Colour:    !*noColour && isFile && logging.TerminalColour(file),
		Level:     slog.LevelInfo,
		KeepLevel: slog.LevelInfo,
	}).Logger("tray")

	host, err := tray.NewHost()
	if err != nil {
		fmt.Fprintf(stderr, "hidi tray: %v\n", err)
		return 1
	}
	defer host.Close()
	if err := tray.Run(ctx, ipc.NewClient(*socket), host, log, tray.Options{}); err != nil {
		fmt.Fprintf(stderr, "hidi tray: %v\n", err)
		return 1
	}
	return 0
}
