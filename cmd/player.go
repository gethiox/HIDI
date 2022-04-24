package main

import (
	"context"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/config/validate"
	mmidi "github.com/moutend/go-midi"
	mmidiev "github.com/moutend/go-midi/event"
)

type Player struct {
	data []byte

	enabledNotes map[uint8]uint8
}

func NewPlayer(data []byte) Player {
	var fixedData []byte
	for i, b := range data {
		if i%2 == 0 {
			fixedData = append(fixedData, b^0b10101010)
		} else {
			fixedData = append(fixedData, b^0b01010101)
		}
	}

	return Player{
		data:         fixedData,
		enabledNotes: make(map[uint8]uint8),
	}
}

func (p *Player) Play(events chan<- midi.Event, ctx context.Context, bpm int) {
	parser := mmidi.NewParser(p.data)
	mevents, err := parser.Parse()
	if err != nil {
		panic(err)
	}

root:
	for _, track := range mevents.Tracks {
		for _, event := range track.Events {
			dt := time.Duration(event.DeltaTime().Quantity().Uint32()) * time.Second / time.Duration(bpm) / 2
			select {
			case <-time.After(dt):
				break
			case <-ctx.Done():
				break root
			}

			switch v := event.(type) {
			case *mmidiev.NoteOnEvent:
				e := v.Serialize()
				p.enabledNotes[uint8(v.Note())] = v.Channel()
				events <- e
			case *mmidiev.NoteOffEvent:
				e := v.Serialize()
				delete(p.enabledNotes, uint8(v.Note()))
				events <- e
			}
		}
	}

	for n, ch := range p.enabledNotes {
		events <- midi.NoteEvent(midi.NoteOff, ch, n, 0)
	}
}

func monitorConfChanges(ctx context.Context, wg *sync.WaitGroup, c <-chan validate.NotifyMessage, events chan midi.Event) {
	defer wg.Done()
	for d := range c {
		p := NewPlayer(d.Data)
		p.Play(events, ctx, d.Bpm)
	}
}
