package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"syscall"
	"time"
)

// ErrNotRunning is returned when no HIDI answers on the control socket.
var ErrNotRunning = errors.New("HIDI is not running")

// Client talks to a running HIDI.
type Client struct {
	path string
	// SnapshotEvery is the least time between two snapshots [Client.Watch]
	// asks for. Zero leaves it to HIDI, which then sends twenty a second
	// at most.
	SnapshotEvery time.Duration
}

// NewClient returns a client for the HIDI that listens at path. Nothing is
// connected until a method is called.
func NewClient(path string) *Client { return &Client{path: path} }

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || refused(err) {
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("connecting to HIDI at %s: %w", c.path, err)
	}
	return conn, nil
}

// ask sends a request that is answered with one snapshot.
func (c *Client) ask(ctx context.Context, op string) (Snapshot, error) {
	return c.send(ctx, request{Op: op})
}

// send makes one request and returns the state HIDI answers with.
func (c *Client) send(ctx context.Context, req request) (Snapshot, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Snapshot{}, fmt.Errorf("asking HIDI: %w", err)
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Snapshot{}, fmt.Errorf("reading HIDI's answer: %w", err)
	}
	switch {
	case resp.Error != "":
		return Snapshot{}, errors.New("HIDI answered: " + resp.Error)
	case resp.Snapshot == nil:
		return Snapshot{}, errors.New("HIDI's answer has no state in it")
	}
	return *resp.Snapshot, nil
}

// Preview makes the running HIDI use text in place of a profile file,
// without the file being written: for trying a change before it is saved.
// It lasts until the file changes on disk; an empty text ends it at once.
func (c *Client) Preview(ctx context.Context, file, text string) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	// Written by hand so that it begins the way the server tells it by.
	rest, err := json.Marshal(struct {
		File string `json:"file"`
		Text string `json:"text"`
	}{file, text})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append([]byte(previewHead+","), append(rest[1:], '\n')...)); err != nil {
		return fmt.Errorf("asking HIDI: %w", err)
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("reading HIDI's answer: %w", err)
	}
	if resp.Error != "" {
		return errors.New("HIDI answered: " + resp.Error)
	}
	return nil
}

// ToggleLearning turns learning mode on if it is off and off if it is on,
// and returns the state afterwards.
func (c *Client) ToggleLearning(ctx context.Context) (Snapshot, error) { return c.ask(ctx, "learn") }

// Simulate asks HIDI to show, or to stop showing, what the lights of an
// instrument's keyboard would show, for a keyboard whose lights HIDI does
// not control. The instrument is named as a snapshot names it.
func (c *Client) Simulate(ctx context.Context, name, kind, location string, on bool) error {
	_, err := c.send(ctx, request{Op: "simulate", Name: name, Type: kind, Location: location, On: on})
	return err
}

// Status returns the current state.
func (c *Client) Status(ctx context.Context) (Snapshot, error) { return c.ask(ctx, "status") }

// SetActive turns HIDI on or off and returns the state afterwards.
func (c *Client) SetActive(ctx context.Context, active bool) (Snapshot, error) {
	if active {
		return c.ask(ctx, "on")
	}
	return c.ask(ctx, "off")
}

// Toggle turns HIDI off if it is on and on if it is off, and returns the
// state afterwards.
func (c *Client) Toggle(ctx context.Context) (Snapshot, error) { return c.ask(ctx, "toggle") }

// Watch follows the running HIDI: it calls onSnapshot with the state now
// and after every change. With logs above zero it also calls onLog with
// the last so many log lines and then with every new one. The functions
// are called one at a time. Watch returns nil when ctx is cancelled, and
// an error when the connection fails or HIDI stops.
func (c *Client) Watch(ctx context.Context, logs int, onSnapshot func(Snapshot), onLog func(LogLine)) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	if err := json.NewEncoder(conn).Encode(request{Op: "watch", Logs: logs, EveryMS: int(c.SnapshotEvery / time.Millisecond)}); err != nil {
		return fmt.Errorf("asking HIDI: %w", err)
	}
	in := json.NewDecoder(bufio.NewReader(conn))
	for {
		var resp response
		if err := in.Decode(&resp); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return errors.New("HIDI stopped")
			}
			return fmt.Errorf("reading from HIDI: %w", err)
		}
		switch {
		case resp.Error != "":
			return errors.New("HIDI answered: " + resp.Error)
		case resp.Snapshot != nil && onSnapshot != nil:
			onSnapshot(*resp.Snapshot)
		case resp.Log != nil && onLog != nil:
			onLog(*resp.Log)
		}
	}
}

// refused reports whether a connection failed because nothing listens: the
// socket file of a HIDI that stopped without removing it.
func refused(err error) bool {
	const wsaECONNREFUSED = 10061 // what Windows reports instead
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == syscall.ECONNREFUSED || errno == wsaECONNREFUSED)
}
