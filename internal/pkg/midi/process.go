package midi

import (
	"context"
	"fmt"

	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi/driver"
	gomidi "gitlab.com/gomidi/midi/v2"
)

type Score struct {
	Score             uint
	MidiEventsEmitted uint
}

func ProcessMidiEvents(ctx context.Context, port driver.Port,
	midiEventsOut <-chan Event, midiEventsIn chan<- Event,
	score *Score) {

	go func() {
		err := port.Output.Open()
		if err != nil {
			panic(err)
		}
		defer port.Output.Close()
		portOut := port.Output.SendChannel()

		var ev Event
		var ok bool
	root:
		for {
			select {
			case <-ctx.Done():
				break root
			case ev, ok = <-midiEventsOut:
				if ok { // todo: investigate
					if ev[0]&0b11110000 == NoteOn {
						score.Score++
					}
				}
			}

			portOut <- ev
			score.MidiEventsEmitted++
		}

		log.Info("Processing output midi events stopped", logger.Debug)
	}()

	go func() {
		err := port.Input.Open()
		if err != nil {
			panic(err)
		}
		defer port.Input.Close()
		var inEvents = make(chan []byte, 10)
		go func() {
			for ev := range port.Input.ReceiveChannel() {
				inEvents <- ev
			}
		}()

	root:
		for {
			var ev []byte
			select {
			case <-ctx.Done():
				break root
			case ev = <-inEvents:
				midiEventsIn <- ev

				msg := gomidi.Message(ev)
				log.Info(fmt.Sprintf("input event: %s (%#v)", msg.String(), ev), logger.Debug)
			}

		}

		log.Info("Processing input midi events stopped", logger.Debug)
	}()
}
