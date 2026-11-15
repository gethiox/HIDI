//go:build linux

package evdev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// WatchOptions configures a [Watcher]. The zero value is ready to use.
type WatchOptions struct {
	// Dir is the directory holding the event nodes. The default is
	// /dev/input.
	Dir string
	// Settle is how long the watcher waits after a node appears before it
	// forms devices, so that all nodes of a device that was just plugged in
	// are there. The default is 250 milliseconds.
	Settle time.Duration
	// Problem, if set, is called when a node cannot be opened, most often
	// for lack of permission. It is called from the watcher's goroutine and
	// at most once for each time the node appears.
	Problem func(path string, err error)
}

// Watcher finds the input devices of the system: those present when it
// starts and those plugged in afterwards.
type Watcher struct {
	opts    WatchOptions
	inotify *os.File
	devices chan *Device
	done    chan struct{}
	stopped sync.WaitGroup
	close   sync.Once
}

// change is a node that appeared, changed its permissions, or disappeared.
type change struct {
	name    string
	removed bool
}

// Watch starts watching for input devices.
func Watch(opts WatchOptions) (*Watcher, error) {
	if opts.Dir == "" {
		opts.Dir = "/dev/input"
	}
	if opts.Settle <= 0 {
		opts.Settle = 250 * time.Millisecond
	}

	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("starting to watch %s: %w", opts.Dir, err)
	}
	// A new node is created first and made accessible by udev a moment
	// later, which shows up as an attribute change.
	if _, err := unix.InotifyAddWatch(fd, opts.Dir, unix.IN_CREATE|unix.IN_ATTRIB|unix.IN_DELETE); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("watching %s: %w", opts.Dir, err)
	}

	w := &Watcher{
		opts:    opts,
		inotify: os.NewFile(uintptr(fd), "inotify"),
		devices: make(chan *Device),
		done:    make(chan struct{}),
	}
	changes := make(chan change, 64)
	w.stopped.Add(2)
	go w.readChanges(changes)
	go w.run(changes)
	return w, nil
}

// Devices returns the channel new devices arrive on. Each device is already
// being read; whoever receives it owns it and must close it. The channel is
// closed when the watcher is closed.
func (w *Watcher) Devices() <-chan *Device { return w.devices }

// Close stops the watcher. Devices it has already delivered are not
// affected.
func (w *Watcher) Close() error {
	w.close.Do(func() {
		close(w.done)
		w.inotify.Close()
		w.stopped.Wait()
		close(w.devices)
	})
	return nil
}

// readChanges turns inotify notifications into changes.
func (w *Watcher) readChanges(changes chan<- change) {
	defer w.stopped.Done()
	buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
	for {
		n, err := w.inotify.Read(buf)
		if err != nil {
			return // the watcher was closed
		}
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
			name := unix.ByteSliceToString(buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+int(ev.Len)])
			off += unix.SizeofInotifyEvent + int(ev.Len)
			if !isEventNode(name) {
				continue
			}
			select {
			case changes <- change{name: name, removed: ev.Mask&unix.IN_DELETE != 0}:
			case <-w.done:
				return
			}
		}
	}
}

func isEventNode(name string) bool {
	rest, ok := strings.CutPrefix(name, "event")
	if !ok || rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// run opens nodes as they appear and forms devices from them.
func (w *Watcher) run(changes <-chan change) {
	defer w.stopped.Done()

	// opened holds the names of nodes that were opened and not yet removed,
	// so that an attribute change does not open a node twice. failed holds
	// the names whose failure was already reported.
	opened := map[string]bool{}
	failed := map[string]bool{}
	// pending holds opened nodes that are not part of a device yet.
	pending := map[string]*Node{}
	defer func() {
		for _, n := range pending {
			n.Close()
		}
	}()

	settle := time.NewTimer(w.opts.Settle)
	defer settle.Stop()

	open := func(name string) {
		if opened[name] {
			return
		}
		path := filepath.Join(w.opts.Dir, name)
		node, err := Open(path)
		if err != nil {
			// A node that vanished again is not worth mentioning.
			if !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.ENODEV) && !failed[name] && w.opts.Problem != nil {
				w.opts.Problem(path, err)
			}
			failed[name] = true
			return
		}
		opened[name] = true
		delete(failed, name)
		pending[name] = node
		settle.Reset(w.opts.Settle)
	}

	// Subscribing before scanning means a node that appears in between is
	// seen twice rather than missed, and opened[] absorbs the duplicate.
	entries, err := os.ReadDir(w.opts.Dir)
	if err != nil && w.opts.Problem != nil {
		w.opts.Problem(w.opts.Dir, err)
	}
	for _, entry := range entries {
		if isEventNode(entry.Name()) {
			open(entry.Name())
		}
	}

	for {
		select {
		case <-w.done:
			return
		case c := <-changes:
			if !c.removed {
				open(c.name)
				continue
			}
			delete(opened, c.name)
			delete(failed, c.name)
			if node, ok := pending[c.name]; ok {
				node.Close()
				delete(pending, c.name)
			}
		case <-settle.C:
			for _, device := range formDevices(pending) {
				select {
				case w.devices <- device:
				case <-w.done:
					device.Close()
				}
			}
			clear(pending)
		}
	}
}

// formDevices groups nodes by the hardware they belong to and returns one
// device for each group, in a stable order.
func formDevices(pending map[string]*Node) []*Device {
	groups := map[groupKey][]*Node{}
	var order []groupKey
	infos := make([]Info, 0, len(pending))
	nodes := map[string]*Node{}
	for _, node := range pending {
		info := node.Info()
		infos = append(infos, info)
		nodes[info.Path] = node
	}
	sortNodes(infos)
	for _, info := range infos {
		key := groupKeyOf(info)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], nodes[info.Path])
	}

	devices := make([]*Device, 0, len(order))
	for _, key := range order {
		devices = append(devices, newDevice(groups[key]))
	}
	return devices
}
