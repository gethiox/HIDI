package driver

import "fmt"

type MIDIPort interface {
	Name() string
	Open() error
	Close() error
}

type MIDIIn interface {
	MIDIPort
	ReceiveChannel() <-chan []byte
}

type MIDIOut interface {
	MIDIPort
	SendChannel() chan<- []byte
}

type Port struct {
	// specific port may be nil if unavailable
	Input  MIDIIn
	Output MIDIOut
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p *Port) String() string {
	if p.Input == nil {
		return fmt.Sprintf("%s (Output only)", p.Output.Name())
	}

	if p.Output == nil {
		return fmt.Sprintf("%s (Input only)", p.Input.Name())
	}

	inName, outName := p.Input.Name(), p.Output.Name()

	var commonPart string

	for i := 0; i < min(len(inName), len(outName)); i++ {
		inR, outR := inName[i], outName[i]

		if inR != outR {
			break
		}
		commonPart += string(inR)
	}
	return fmt.Sprintf("%s (Input/Output)", commonPart)
}
