package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func bigSnapshot() Snapshot {
	s := Snapshot{Version: "hidi 2.0.0 (abc1234) go1.27 linux/amd64", Active: true}
	for i := range 7 {
		in := Instrument{Name: fmt.Sprintf("Device %d", i), Type: "gamepad", Hardware: "0003:231d:0200:0111", Location: "usb-0000:00:14.0-2", Profile: "Default gamepad", ProfileFile: "/home/player/.config/HIDI/profiles/gamepad.toml", Channel: 1, Velocity: 64, Layout: "Default", Notes: []int{60, 64, 67}}
		n := 30
		if i < 2 {
			n, in.Type = 104, "keyboard"
		}
		for b := range n {
			in.Buttons = append(in.Buttons, Button{Name: fmt.Sprintf("KEY_SOMETHING%d", b), Down: b%7 == 0})
		}
		if i >= 2 {
			for a := range 15 {
				in.Axes = append(in.Axes, Axis{Name: fmt.Sprintf("ABS_HAT%dX", a), Value: 0.123456})
			}
		}
		s.Instruments = append(s.Instruments, in)
	}
	return s
}

func BenchmarkSnapshotEncode(b *testing.B) {
	s := bigSnapshot()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	b.ReportAllocs()
	for b.Loop() {
		buf.Reset()
		enc.Encode(response{Snapshot: &s})
	}
	b.ReportMetric(float64(buf.Len()), "bytes")
}

func BenchmarkSnapshotDecode(b *testing.B) {
	s := bigSnapshot()
	data, _ := json.Marshal(response{Snapshot: &s})
	b.ReportAllocs()
	for b.Loop() {
		var r response
		json.Unmarshal(data, &r)
	}
}
