package alsa

import (
	"fmt"
	"sort"

	"github.com/gethiox/HIDI/internal/pkg/midi/driver"
	gomidi "gitlab.com/gomidi/midi/v2"

	"gitlab.com/gomidi/midi/v2/drivers"
	"gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv" // autoregisters driver
)

type MIDIInPortFromDriver struct {
	c        chan []byte
	port     drivers.In
	stopFunc func()
}

func (in *MIDIInPortFromDriver) Name() string {
	return in.port.String()
}

func (in *MIDIInPortFromDriver) Open() error {
	err := in.port.Open()
	if err != nil {
		return fmt.Errorf("failed to open input: %w", err)
	}

	stopFn, err := in.port.Listen(func(msg []byte, milliseconds int32) {
		in.c <- msg
	}, drivers.ListenConfig{
		TimeCode:        true,
		ActiveSense:     true,
		SysEx:           true,
		SysExBufferSize: 0,
		OnErr:           func(err error) {},
	})

	if err != nil {
		return fmt.Errorf("failed to listen on device: %w", err)
	}
	in.stopFunc = stopFn
	return nil
}

func (in *MIDIInPortFromDriver) Close() error {
	in.stopFunc()
	close(in.c)
	return in.port.Close()
}

func (in *MIDIInPortFromDriver) ReceiveChannel() <-chan []byte {
	return in.c
}

func NewMIDIInPortFromDriver(in drivers.In) (driver.MIDIIn, error) {
	port := &MIDIInPortFromDriver{
		c:    make(chan []byte, 16),
		port: in,
	}
	return port, nil

}

type MIDIOutPortFromDriver struct {
	c    chan []byte
	port drivers.Out
}

func (out *MIDIOutPortFromDriver) Name() string {
	return out.port.String()
}

func (out *MIDIOutPortFromDriver) Open() error {
	err := out.port.Open()
	if err != nil {
		return fmt.Errorf("failed to open output: %w", err)
	}

	go func() {
		for event := range out.c {
			_ = out.port.Send(event)
		}
	}()
	return nil
}

func (out *MIDIOutPortFromDriver) Close() error {
	close(out.c)
	return out.port.Close()
}

func (out *MIDIOutPortFromDriver) SendChannel() chan<- []byte {
	return out.c
}

func NewMIDIOutPortFromDriver(out drivers.Out) (driver.MIDIOut, error) {
	port := &MIDIOutPortFromDriver{
		c:    make(chan []byte, 16),
		port: out,
	}

	return port, nil
}

func CreatePort(name string) (driver.Port, error) {
	d := drivers.Get()
	if d == nil {
		return driver.Port{}, fmt.Errorf("failed to get driver")
	}

	rtmidid, ok := d.(*rtmididrv.Driver)
	if !ok {
		return driver.Port{}, fmt.Errorf("failed to convert driver")
	}

	in, err := rtmidid.OpenVirtualIn(name)
	if err != nil {
		return driver.Port{}, fmt.Errorf("failed to open virtual input: %v", err)
	}
	out, err := rtmidid.OpenVirtualOut(name)
	if err != nil {
		return driver.Port{}, fmt.Errorf("failed to open virtual output: %v", err)
	}

	inPort, err := NewMIDIInPortFromDriver(in)
	if err != nil {
		return driver.Port{}, fmt.Errorf("failed to open input driver: %v", err)
	}

	outPort, err := NewMIDIOutPortFromDriver(out)
	if err != nil {
		return driver.Port{}, fmt.Errorf("failed to open output driver: %v", err)
	}

	return driver.Port{
		Input:  inPort,
		Output: outPort,
	}, nil
}

func GetPorts() []driver.Port {
	inPorts := gomidi.GetInPorts()
	outPorts := gomidi.GetOutPorts()

	var ports = make([]driver.Port, 0)

	var TotalUniquePortNumbers = make(map[int]struct{})

	var inPortMap = make(map[int]int)
	var outPortMap = make(map[int]int)

	for i, p := range inPorts {
		inPortMap[p.Number()] = i
		TotalUniquePortNumbers[p.Number()] = struct{}{}
	}

	for i, p := range outPorts {
		outPortMap[p.Number()] = i
		TotalUniquePortNumbers[p.Number()] = struct{}{}
	}

	var sortedPortNumbers = make([]int, 0, len(TotalUniquePortNumbers))

	for pNumber := range TotalUniquePortNumbers {
		sortedPortNumbers = append(sortedPortNumbers, pNumber)
	}

	sort.Ints(sortedPortNumbers)

	for _, pNumber := range sortedPortNumbers {
		var in drivers.In = nil
		var out drivers.Out = nil
		var outPort driver.MIDIOut = nil
		var inPort driver.MIDIIn = nil
		var err error

		idx, ok := inPortMap[pNumber]
		if ok {
			in = inPorts[idx]
			inPort, err = NewMIDIInPortFromDriver(in)
			if err != nil {
				panic(err)
			}
		}

		idx, ok = outPortMap[pNumber]
		if ok {
			out = outPorts[idx]
			outPort, err = NewMIDIOutPortFromDriver(out)
			if err != nil {
				panic(err)
			}
		}

		ports = append(ports, driver.Port{
			Input:  inPort,
			Output: outPort,
		})
	}

	return ports
}

// PickMidiPort returns midi port pair for n-th (idx) device or creates virtual one.
func PickMidiPort(idx int) (driver.Port, error) {

	midiPorts := GetPorts()

	if idx < 0 || idx+1 > len(midiPorts) {
		fmt.Printf("There is no midi devices available, we're deeply sorry\n")
		return driver.Port{}, fmt.Errorf("midi port ID %d doesn't exist", idx)
	}

	return midiPorts[idx], nil

}
