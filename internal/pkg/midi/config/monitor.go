package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/fsnotify/fsnotify"
)

func DetectDeviceConfigChanges(ctx context.Context) <-chan bool {
	var change = make(chan bool)

	go func() {
		defer close(change)
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return
		}

		go func() {
			<-ctx.Done()
			err := watcher.Close()
			if err != nil {
				log.Info(fmt.Sprintf("closing watched failed: %v", err))
			}
		}()

		for _, path := range []string{
			factoryGamepad,
			factoryKeyboard,
			userGamepad,
			userKeyboard,
		} {
			err = watcher.Add(path)
		}

		for event := range watcher.Events {
			if event.Op != fsnotify.Write {
				continue
			}

			name := strings.ToLower(event.Name)
			if strings.HasSuffix(name, "yml") || strings.HasSuffix(name, "yaml") {
				log.Info(fmt.Sprintf("config change detected: %s", event.Name))
				change <- true
			}
		}
	}()

	return change
}
