//go:build linux

package evdev

import (
	"errors"
	"fmt"
	"sync"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/input/evdev/uinput"
)

// PassPhys is the physical location that the keyboards HIDI creates to
// pass keys on report. The watcher leaves devices with it alone: they
// carry what HIDI itself typed.
const PassPhys = "hidi/pass"

// passing passes the keys of a device on to the system through a keyboard
// made in software, except for some that are hidden. The kernel offers no
// way to keep single keys of a device from the system: a device is either
// grabbed, and then the system sees none of it, or it is not. So the
// device is grabbed, and what the system should see is typed again on a
// keyboard of HIDI's own.
//
// Only nodes that have nothing but keys are treated so. A node with axes,
// such as the volume wheel or the pointer some keyboards have, stays with
// the system, because movements cannot be passed on this way.
type passing struct {
	mu     sync.Mutex
	on     bool
	out    *uinput.Device
	hidden map[uint16]bool
	// only, if not nil, holds the keys that are passed on; every other
	// key is kept back, whatever hidden says.
	only  map[uint16]bool
	nodes map[*Node]bool // the nodes whose keys are passed on
	down  map[uint16]bool
	// closed is set when the device has ended; nothing is passed on any
	// more, and no keyboard is made for it.
	closed bool
}

// Pass keeps the hidden keys from the rest of the system and passes every
// other key on. It fails if the keys cannot be typed again, for lack of
// access to /dev/uinput, or if a hidden key is on a node that cannot be
// held; the device is then left as it was.
//
// As with [Node.Grab], hiding begins once no key of a node is down.
func (d *Device) Pass(hidden []control.Code) error {
	p := &d.pass
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	nodes, keys, have := d.keyNodes()
	set := map[uint16]bool{}
	for _, k := range hidden {
		if k.Kind != control.KindKey || !have[k.Num] {
			return fmt.Errorf("%v is not among the keys of the device that can be held back", k)
		}
		set[k.Num] = true
	}
	if err := d.passKeyboard(keys); err != nil {
		return err
	}
	for i, n := range d.nodes {
		var err error
		if nodes[n] {
			err = n.Grab()
		} else {
			err = n.Release()
		}
		if err != nil {
			for _, undone := range d.nodes[:i+1] {
				undone.Release()
			}
			return err
		}
	}
	// Keys that were passed on as pressed and are hidden now are let go
	// of; the others stay as they are.
	for code := range p.down {
		if set[code] {
			p.out.Key(code, false)
			delete(p.down, code)
		}
	}
	if p.down == nil {
		p.down = map[uint16]bool{}
	}
	p.on, p.hidden, p.only, p.nodes = true, set, nil, nodes
	return nil
}

// keyNodes returns the nodes that have nothing but keys, whose keys can
// be typed again, and those keys.
func (d *Device) keyNodes() (nodes map[*Node]bool, keys []uint16, have map[uint16]bool) {
	nodes, have = map[*Node]bool{}, map[uint16]bool{}
	for _, n := range d.nodes {
		info := n.Info()
		if len(info.Axes) > 0 || info.Relative || len(info.Keys) == 0 {
			continue
		}
		nodes[n] = true
		for _, k := range info.Keys {
			if !have[k.Num] {
				have[k.Num] = true
				keys = append(keys, k.Num)
			}
		}
	}
	return nodes, keys, have
}

// passKeyboard makes the keyboard the keys are typed again on, if it is
// not there yet. The caller holds d.pass.mu.
func (d *Device) passKeyboard(keys []uint16) error {
	p := &d.pass
	if p.out != nil {
		return nil
	}
	out, err := uinput.Create(uinput.Spec{
		Name:    d.info.Name + " (through HIDI)",
		Phys:    PassPhys,
		Vendor:  d.info.Hardware.Vendor,
		Product: d.info.Hardware.Product,
		Version: d.info.Hardware.Version,
		Keys:    keys,
	})
	if err != nil {
		return fmt.Errorf("making a keyboard to pass the keys on: %w", err)
	}
	p.out = out
	return nil
}

// Share keeps the device from the rest of the system like [Device.Grab],
// except for the open keys, which are typed again on a keyboard of HIDI's
// own. It fails if that keyboard cannot be made, for lack of access to
// /dev/uinput; the device is then left as it was.
func (d *Device) Share(open []control.Code) error {
	p := &d.pass
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	nodes, keys, have := d.keyNodes()
	set := map[uint16]bool{}
	for _, k := range open {
		// A key the device does not have, or has on a node whose keys
		// cannot be typed again, is simply never pressed.
		if k.Kind == control.KindKey && have[k.Num] {
			set[k.Num] = true
		}
	}
	if err := d.passKeyboard(keys); err != nil {
		return err
	}
	for i, n := range d.nodes {
		if err := n.Grab(); err != nil {
			for _, undone := range d.nodes[:i+1] {
				undone.Release()
			}
			return err
		}
	}
	// Keys that were passed on as pressed and are kept back now are let
	// go of.
	for code := range p.down {
		if !set[code] {
			p.out.Key(code, false)
			delete(p.down, code)
		}
	}
	if p.down == nil {
		p.down = map[uint16]bool{}
	}
	p.on, p.hidden, p.only, p.nodes = true, nil, set, nodes
	return nil
}

// Type presses or releases a key on the keyboard that passes keys on.
func (d *Device) Type(key control.Code, down bool) error {
	p := &d.pass
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.out == nil {
		return errors.New("there is no keyboard to type on; the device was never passed on")
	}
	if key.Kind != control.KindKey {
		return fmt.Errorf("%v cannot be typed", key)
	}
	return p.out.Key(key.Num, down)
}

// forward passes on what one node reported.
func (p *passing) forward(node *Node, events []control.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// A grab that waits for a key to come up is not in place yet, and the
	// system still gets the node's keys by itself. That holds for the
	// events with which the grab began as well.
	if !p.on || !p.nodes[node] || !node.HeldBefore() {
		return
	}
	for _, ev := range events {
		if ev.Code.Kind != control.KindKey || p.hidden[ev.Code.Num] || p.only != nil && !p.only[ev.Code.Num] {
			continue
		}
		down := ev.Pressed()
		if down == p.down[ev.Code.Num] {
			continue
		}
		if p.out.Key(ev.Code.Num, down) != nil {
			continue
		}
		if down {
			p.down[ev.Code.Num] = true
		} else {
			delete(p.down, ev.Code.Num)
		}
	}
}

// stop ends the passing on. Keys that were passed on as pressed are
// released, so that the system does not take them for held.
func (p *passing) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.on {
		return
	}
	for code := range p.down {
		p.out.Key(code, false)
	}
	p.on, p.down = false, nil
}

// close removes the keyboard.
func (p *passing) close() {
	p.stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.out != nil {
		p.out.Close()
		p.out = nil
	}
}
