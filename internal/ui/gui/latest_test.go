package gui

import (
	"sync"
	"testing"
	"time"
)

func TestLatest(t *testing.T) {
	var (
		mu      sync.Mutex
		got     = map[string][]int{}
		release = make(chan struct{})
		started = make(chan struct{}, 100)
	)
	l := latest[string, int]{hand: func(key string, value int) {
		started <- struct{}{}
		<-release
		mu.Lock()
		got[key] = append(got[key], value)
		mu.Unlock()
	}}
	// The first is taken at once; of those that follow while it is being
	// handed over, the newest of each key is kept.
	l.put("a", 1)
	<-started
	for i := 2; i <= 50; i++ {
		l.put("a", i)
	}
	l.put("b", 7)
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		done := len(got["a"]) == 2 && len(got["b"]) == 1
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("handed over: %v", got)
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if got["a"][0] != 1 || got["a"][1] != 50 || got["b"][0] != 7 {
		t.Errorf("handed over: %v", got)
	}
	// And it starts again after it has run dry.
	l.put("c", 3)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Error("nothing was handed over after a pause")
	}
}
