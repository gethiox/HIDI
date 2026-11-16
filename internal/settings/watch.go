package settings

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Watch calls changed with the result of [Load] every time the settings
// file changes, until ctx is cancelled. It looks at the file once per
// interval, and passes a change on when the file has stayed the same for
// one more interval, so that it is not read while it is being written.
func Watch(ctx context.Context, path string, interval time.Duration, changed func(Settings, error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	loaded := stamp(path)
	seen := loaded
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := stamp(path)
		settled := now == seen
		seen = now
		if !settled || now == loaded {
			continue
		}
		loaded = now
		changed(Load(path))
	}
}

// stamp describes a file by its size and the time it was written.
func stamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	return fmt.Sprintf("%d %d", info.Size(), info.ModTime().UnixNano())
}
