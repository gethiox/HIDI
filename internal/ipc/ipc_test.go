package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHost is a running HIDI as far as the server can tell.
type fakeHost struct {
	changes Notifier

	mu     sync.Mutex
	active bool
	notes  int
	learn  bool
	log    []LogLine
	// previewed is what was last asked to be tried.
	previewed [2]string
	// simulated is the last request to show lights, as one text.
	simulated string
}

func (h *fakeHost) host() Host {
	return Host{
		Snapshot: func() Snapshot {
			h.mu.Lock()
			defer h.mu.Unlock()
			return Snapshot{Version: "test", Active: h.active, Learning: h.learn, Instruments: []Instrument{
				{Name: "Keyboard", Type: "keyboard", Profile: "Default keyboard", Channel: 1, Sounding: h.notes},
			}}
		},
		SetActive: func(active bool) {
			h.mu.Lock()
			h.active = active
			h.mu.Unlock()
			h.changes.Notify()
		},
		Changes: &h.changes,
		SetLearning: func(on bool) {
			h.mu.Lock()
			h.learn = on
			h.mu.Unlock()
			h.changes.Notify()
		},
		Simulate: func(name, kind, location string, on bool) {
			h.mu.Lock()
			h.simulated = fmt.Sprintf("%s|%s|%s|%v", name, kind, location, on)
			h.mu.Unlock()
		},
		Preview: func(file, text string) error {
			if text == "broken" {
				return errors.New("line 1: this is no profile")
			}
			h.mu.Lock()
			h.previewed = [2]string{file, text}
			h.mu.Unlock()
			return nil
		},
		LogLines: func(from, to int) []LogLine {
			h.mu.Lock()
			defer h.mu.Unlock()
			return append([]LogLine(nil), h.log[max(from, 0):min(to, len(h.log))]...)
		},
		LogTotal: func() int {
			h.mu.Lock()
			defer h.mu.Unlock()
			return len(h.log)
		},
	}
}

func (h *fakeHost) play(notes int) {
	h.mu.Lock()
	h.notes = notes
	h.mu.Unlock()
	h.changes.Notify()
}

func (h *fakeHost) logf(format string, args ...any) {
	h.mu.Lock()
	h.log = append(h.log, LogLine{Seq: len(h.log), Level: "INFO", Text: fmt.Sprintf(format, args...)})
	h.mu.Unlock()
}

// start runs a server for a fake host and returns a client for it.
func start(t *testing.T) (*fakeHost, *Client, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hidi", "control.sock")
	server, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHost{active: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		server.Serve(ctx, h.host())
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the server did not stop")
		}
	})
	return h, NewClient(path), path
}

func TestStatusAndSwitching(t *testing.T) {
	_, client, _ := start(t)
	ctx := t.Context()

	s, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Active || s.Version != "test" || len(s.Instruments) != 1 || s.Instruments[0].Name != "Keyboard" {
		t.Errorf("Status = %+v", s)
	}
	if s, err = client.SetActive(ctx, false); err != nil || s.Active {
		t.Errorf("SetActive(false) = %+v, %v", s, err)
	}
	if s, err = client.Toggle(ctx); err != nil || !s.Active {
		t.Errorf("Toggle = %+v, %v", s, err)
	}
	if s, err = client.Toggle(ctx); err != nil || s.Active {
		t.Errorf("second Toggle = %+v, %v", s, err)
	}
	if s, err = client.SetActive(ctx, true); err != nil || !s.Active {
		t.Errorf("SetActive(true) = %+v, %v", s, err)
	}
}

func TestNotRunning(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewClient(filepath.Join(dir, "absent.sock")).Status(t.Context()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("without a socket: %v, want ErrNotRunning", err)
	}

	// A socket nobody listens on any more, as a killed HIDI leaves it.
	path := filepath.Join(dir, "stale.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("this system does not keep the socket file: %v", err)
	}
	if _, err := NewClient(path).Status(t.Context()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("with a stale socket: %v, want ErrNotRunning", err)
	}

	// A new HIDI takes the place of the stale socket.
	server, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen over a stale socket: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Close left the socket behind: %v", err)
	}
}

func TestOnlyOneServer(t *testing.T) {
	_, _, path := start(t)
	if _, err := Listen(path); !errors.Is(err, ErrRunning) {
		t.Errorf("second Listen = %v, want ErrRunning", err)
	}
}

func TestSocketIsPrivate(t *testing.T) {
	_, _, path := start(t)
	if os.PathSeparator == '\\' {
		t.Skip("Windows keeps a user's directories private by other means")
	}
	for _, p := range []string{filepath.Dir(path), path} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s has permissions %o; others must have none", p, perm)
		}
	}
}

func TestSocketIsRemovedOnStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	server, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		server.Serve(ctx, (&fakeHost{}).host())
		close(done)
	}()
	cancel()
	<-done
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket is still there after the server stopped: %v", err)
	}
}

func TestWatch(t *testing.T) {
	h, client, _ := start(t)
	for i := range 5 {
		h.logf("old %d", i)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	snapshots := make(chan Snapshot, 64)
	lines := make(chan LogLine, 64)
	done := make(chan error, 1)
	go func() {
		done <- client.Watch(ctx, 2, func(s Snapshot) { snapshots <- s }, func(l LogLine) { lines <- l })
	}()
	nextSnapshot := func(what string, cond func(Snapshot) bool) {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case s := <-snapshots:
				if cond(s) {
					return
				}
			case <-timeout:
				t.Fatalf("no snapshot with %s", what)
			}
		}
	}
	nextLine := func(want string) {
		t.Helper()
		select {
		case l := <-lines:
			if l.Text != want {
				t.Fatalf("log line %q, want %q", l.Text, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no log line %q", want)
		}
	}

	// The state now, and the last two lines of the log.
	nextSnapshot("the first state", func(s Snapshot) bool { return s.Active && s.Instruments[0].Sounding == 0 })
	nextLine("old 3")
	nextLine("old 4")

	h.play(3)
	nextSnapshot("three notes", func(s Snapshot) bool { return s.Instruments[0].Sounding == 3 })
	h.logf("new line")
	nextLine("new line")

	// A burst of changes arrives as few snapshots, the last one current.
	for n := range 200 {
		h.play(n)
	}
	h.play(7)
	nextSnapshot("the final state of a burst", func(s Snapshot) bool { return s.Instruments[0].Sounding == 7 })
	if n := len(snapshots); n > 20 {
		t.Errorf("%d snapshots are queued after a burst; they should have merged", n)
	}

	// Another client's change is seen too.
	if _, err := client.SetActive(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	nextSnapshot("HIDI off", func(s Snapshot) bool { return !s.Active })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Watch returned %v after its context was cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not return")
	}
}

func TestWatchWithoutLogs(t *testing.T) {
	h, client, _ := start(t)
	h.logf("a line")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	got := make(chan Snapshot, 8)
	go client.Watch(ctx, 0, func(s Snapshot) { got <- s }, func(LogLine) { t.Error("a log line arrived unasked") })
	<-got
	h.logf("another")
	time.Sleep(3 * logInterval)
}

// A watcher learns that HIDI stopped.
func TestWatchEndsWhenServerStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	server, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		server.Serve(ctx, (&fakeHost{}).host())
		close(served)
	}()

	first := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- NewClient(path).Watch(t.Context(), 0, func(Snapshot) {
			select {
			case first <- struct{}{}:
			default:
			}
		}, nil)
	}()
	<-first
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stopped") {
			t.Errorf("Watch returned %v, want it to say that HIDI stopped", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not notice the server stopping")
	}
	<-served
}

// What a client that does not use this package sees on the wire.
func TestProtocol(t *testing.T) {
	_, _, path := start(t)
	ask := func(line string) map[string]any {
		t.Helper()
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
		answer, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatalf("after %q: %v", line, err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(answer), &m); err != nil {
			t.Fatalf("answer %q is not JSON: %v", answer, err)
		}
		return m
	}

	if m := ask(`{"op":"status"}` + "\n"); m["snapshot"] == nil || m["error"] != nil {
		t.Errorf("status: %v", m)
	}
	if m := ask(`{"op":"status","something":"new"}` + "\n"); m["snapshot"] == nil {
		t.Errorf("a request with an unknown field: %v", m)
	}
	for name, line := range map[string]string{
		"unknown operation": `{"op":"explode"}` + "\n",
		"not JSON":          "hello\n",
		"too long":          `{"op":"` + strings.Repeat("x", 2*maxRequest) + `"}` + "\n",
	} {
		if m := ask(line); m["error"] == nil || m["snapshot"] != nil {
			t.Errorf("%s: %v", name, m)
		}
	}
}

func TestNotifier(t *testing.T) {
	var n Notifier
	n.Notify() // nobody listens
	a, stopA := n.Listen()
	b, stopB := n.Listen()
	defer stopB()
	n.Notify()
	n.Notify() // merges with the first
	for _, ch := range []<-chan struct{}{a, b} {
		select {
		case <-ch:
		default:
			t.Fatal("a listener was not signalled")
		}
		select {
		case <-ch:
			t.Fatal("two signals were kept instead of one")
		default:
		}
	}
	stopA()
	n.Notify()
	select {
	case <-a:
		t.Error("a listener that stopped was signalled")
	default:
	}
	<-b
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join("run", "user", "1000"))
	path, err := DefaultPath()
	if err != nil || path != filepath.Join("run", "user", "1000", "hidi", "control.sock") {
		t.Errorf("DefaultPath = %q, %v", path, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if path, err := DefaultPath(); err == nil && filepath.Base(path) != "control.sock" {
		t.Errorf("DefaultPath without a runtime directory = %q", path)
	}
}

func TestSnapshotPace(t *testing.T) {
	for _, c := range []struct {
		ms   int
		want time.Duration
	}{
		{0, snapshotInterval},
		{-3, snapshotInterval},
		{1, minSnapshotInterval},
		{4, 4 * time.Millisecond},
		{1000000, maxSnapshotInterval},
	} {
		if got := snapshotPace(c.ms); got != c.want {
			t.Errorf("snapshotPace(%d) = %v, want %v", c.ms, got, c.want)
		}
	}
}

// Of two HIDIs that start at the same moment, one gets the socket and the
// other is told that HIDI runs.
func TestListenAtTheSameMoment(t *testing.T) {
	for round := range 200 {
		path := filepath.Join(t.TempDir(), "control.sock")
		results := make(chan error, 2)
		start := make(chan struct{})
		var servers [2]*Server
		for i := range 2 {
			go func() {
				<-start
				var err error
				servers[i], err = Listen(path)
				results <- err
			}()
		}
		close(start)
		a, b := <-results, <-results
		won := 0
		for _, err := range []error{a, b} {
			switch {
			case err == nil:
				won++
			case !errors.Is(err, ErrRunning):
				t.Fatalf("round %d: Listen failed with %v", round, err)
			}
		}
		if won != 1 {
			t.Fatalf("round %d: %d of two got the socket", round, won)
		}
		for _, s := range servers {
			if s != nil {
				s.Close()
			}
		}
	}
}

// A profile is tried through the socket: its text arrives whole, also when
// it is far longer than any other request may be, and what HIDI makes of
// it comes back.
func TestPreview(t *testing.T) {
	h, client, _ := start(t)
	ctx := t.Context()
	long := "name = \"A \\\"long\\\" profile\"\n" + strings.Repeat("# a line of a profile that has many of them\n", 5000)
	if len(long) < 20*maxRequest {
		t.Fatal("the text is not long enough to mean anything")
	}
	if err := client.Preview(ctx, "/profiles/keyboard.toml", long); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	got := h.previewed
	h.mu.Unlock()
	if got[0] != "/profiles/keyboard.toml" || got[1] != long {
		t.Errorf("the host got file %q and %d bytes of text, want %d", got[0], len(got[1]), len(long))
	}
	if err := client.Preview(ctx, "/profiles/keyboard.toml", "broken"); err == nil || !strings.Contains(err.Error(), "this is no profile") {
		t.Errorf("a text that is no profile: %v", err)
	}
	// Ending it is the same request without a text.
	if err := client.Preview(ctx, "/profiles/keyboard.toml", ""); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	got = h.previewed
	h.mu.Unlock()
	if got[1] != "" {
		t.Errorf("after ending, the host still has %d bytes", len(got[1]))
	}
}

// Learning mode is turned on and off through the socket, and the state
// says how it stands.
func TestLearningSwitch(t *testing.T) {
	_, client, _ := start(t)
	ctx := t.Context()
	if s, err := client.Status(ctx); err != nil || s.Learning {
		t.Fatalf("at first: %+v, %v", s.Learning, err)
	}
	if s, err := client.ToggleLearning(ctx); err != nil || !s.Learning {
		t.Errorf("turned on: %v, %v", s.Learning, err)
	}
	if s, err := client.ToggleLearning(ctx); err != nil || s.Learning {
		t.Errorf("turned off again: %v, %v", s.Learning, err)
	}
}

func TestSimulate(t *testing.T) {
	h, client, _ := start(t)
	if err := client.Simulate(t.Context(), "Some Keyboard", "keyboard", "usb-1/input0", true); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	got := h.simulated
	h.mu.Unlock()
	if got != "Some Keyboard|keyboard|usb-1/input0|true" {
		t.Errorf("the host was told %q", got)
	}
}
