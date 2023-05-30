package utils

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/amenzhinsky/go-memexec"
	"github.com/gethiox/HIDI/internal/pkg/logger"
)

var log = logger.GetLogger()

func RunBinary(wg *sync.WaitGroup, ctx context.Context, openRGB []byte, port int) {
	defer wg.Done()
	exe, err := memexec.New(openRGB)
	if err != nil {
		panic(err)
	}

	defer func() {
		err := exe.Close()
		if err != nil {
			log.Info(fmt.Sprintf("failed to close memory exec: %s", err), logger.Error)
		}
	}()

	cmd := exe.Command("--server", "--noautoconnect", "--server-port", fmt.Sprintf("%d", port))

	out1, in1 := io.Pipe()
	out2, in2 := io.Pipe()

	defer out1.Close()
	defer in1.Close()
	defer out2.Close()
	defer in2.Close()

	cmd.Stdout = in1
	cmd.Stderr = in2

	log.Info("[OpenRGB] start", logger.Debug)
	err = cmd.Start()
	if err != nil {
		log.Info(fmt.Sprintf("[OpenRGB] Failed to start: %s", err), logger.Error)
		return
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		time.Sleep(time.Millisecond * 200)
		err := cmd.Process.Signal(os.Interrupt)
		if err != nil {
			if !errors.Is(err, os.ErrProcessDone) {
				log.Info(fmt.Sprintf("[OpenRGB] failed to send signal: %s", err), logger.Error)
			}
		} else {
			log.Info("[OpenRGB] interrupt success", logger.Info)
		}
	}()

	go func() {
		scan1 := bufio.NewScanner(out1)
		for scan1.Scan() {
			log.Info("[OpenRGB] o> "+scan1.Text(), logger.Debug)
		}
	}()

	go func() {
		scan2 := bufio.NewScanner(out2)
		for scan2.Scan() {
			log.Info("[OpenRGB] e> "+scan2.Text(), logger.Debug)
		}
	}()

	err = cmd.Wait()
	if err != nil {
		log.Info(fmt.Sprintf("[OpenRGB] Execution error: %s", err), logger.Error)
	}
	log.Info("[OpenRGB] Done", logger.Debug)
}
