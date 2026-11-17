//go:build linux

package evdev

import (
	"errors"
	"sync"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/input"
)

var (
	_ input.Device = (*Device)(nil)
	_ input.Passer = (*Device)(nil)
)

// ErrClosed is reported by [Device.Err] after the device was closed.
var ErrClosed = errors.New("input device closed")

// Device is one piece of input hardware, made of all the event nodes the
// kernel created for it. It implements [input.Device].
type Device struct {
	info  control.DeviceInfo
	nodes []*Node
	parts []string

	events chan control.Event
	// stop is closed when the device shuts down, for any reason.
	stop     chan struct{}
	stopOnce sync.Once
	readers  sync.WaitGroup

	mu  sync.Mutex
	err error

	// pass is how the device's keys are passed on to the system while it
	// is held for the sake of a hidden key; see [Device.Pass].
	pass passing
}

// newDevice merges nodes into a device and starts reading them. The nodes
// must belong to the same hardware. The device takes ownership of them.
func newDevice(nodes []*Node) *Device {
	infos := make([]Info, len(nodes))
	for i, n := range nodes {
		infos[i] = n.Info()
	}
	d := &Device{
		info:   describeDevice(infos),
		nodes:  nodes,
		parts:  partNames(infos),
		events: make(chan control.Event, 256),
		stop:   make(chan struct{}),
	}
	for i := range nodes {
		d.readers.Add(1)
		go d.read(nodes[i], d.parts[i])
	}
	go func() {
		d.readers.Wait()
		close(d.events)
	}()
	return d
}

// read forwards the events of one node until the device shuts down.
func (d *Device) read(node *Node, part string) {
	defer d.readers.Done()
	var buf []control.Event
	for {
		var err error
		buf, err = node.Read(buf[:0])
		d.pass.forward(node, buf)
		for _, ev := range buf {
			ev.Part = part
			select {
			case d.events <- ev:
			case <-d.stop:
				return
			}
		}
		if err != nil {
			// Losing one node means the hardware is gone.
			d.shutdown(err)
			return
		}
	}
}

// shutdown stops the device, recording the first reason given.
func (d *Device) shutdown(reason error) {
	d.stopOnce.Do(func() {
		d.mu.Lock()
		d.err = reason
		d.mu.Unlock()
		close(d.stop)
		// Closing the nodes interrupts the readers and ends any grab.
		for _, n := range d.nodes {
			n.Close()
		}
		d.pass.close()
	})
}

// Info describes the device.
func (d *Device) Info() control.DeviceInfo { return d.info }

// Nodes describes the event nodes the device is made of, in the order of
// their node numbers.
func (d *Device) Nodes() []Info {
	infos := make([]Info, len(d.nodes))
	for i, n := range d.nodes {
		infos[i] = n.Info()
	}
	return infos
}

// Events returns the channel the device's input arrives on. Events from a
// node that forms a named part carry that name in [control.Event.Part]. The
// channel is closed when the device is unplugged or closed.
func (d *Device) Events() <-chan control.Event { return d.events }

// Err returns why the device stopped: [ErrDisconnected] if it was unplugged,
// [ErrClosed] if it was closed, or another error if reading failed. It
// returns nil while the device is running.
func (d *Device) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

// Grab takes every node of the device away from the rest of the system. As
// described at [Node.Grab], the grab of a node takes effect once none of its
// keys is down. If one node cannot be grabbed, none stays grabbed.
func (d *Device) Grab() error {
	d.pass.stop()
	for i, n := range d.nodes {
		if err := n.Grab(); err != nil {
			// The node that failed is released too: it still wants the
			// grab, and would take it once whatever held it lets go.
			for _, grabbed := range d.nodes[:i+1] {
				grabbed.Release()
			}
			return err
		}
	}
	return nil
}

// Release ends a grab on every node.
func (d *Device) Release() error {
	d.pass.stop()
	var errs []error
	for _, n := range d.nodes {
		if err := n.Release(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close stops reading the device and ends a grab. It returns once the
// Events channel no longer receives anything new. Closing a device twice is
// not an error.
func (d *Device) Close() error {
	d.shutdown(ErrClosed)
	d.readers.Wait()
	return nil
}
