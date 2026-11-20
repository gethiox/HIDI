package ipc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultPath returns where the control socket of the user's HIDI lives.
// It is in the directory the system keeps for the user's running programs
// where there is one ($XDG_RUNTIME_DIR on Linux), and in the user's cache
// directory otherwise.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("finding a place for the control socket: %w", err)
		}
		dir = cache
	}
	return filepath.Join(dir, "hidi", "control.sock"), nil
}

// ErrRunning is returned by [Listen] when another HIDI answers on the
// socket.
var ErrRunning = errors.New("HIDI is already running")

// These intervals pace what a watching client is sent. Snapshots change
// with every note; a client needs them at the rate a person can read,
// unless it asks for another: a window that draws moving axes wants them
// as often as its monitor shows a picture. The log has no way to announce
// a new line, so it is looked at while someone watches.
const (
	snapshotInterval    = 50 * time.Millisecond
	minSnapshotInterval = 2 * time.Millisecond
	maxSnapshotInterval = 5 * time.Second
	logInterval         = 100 * time.Millisecond
)

// snapshotPace is the time between two snapshots for a client that asked
// for one every so many milliseconds.
func snapshotPace(everyMS int) time.Duration {
	if everyMS <= 0 {
		return snapshotInterval
	}
	return min(max(time.Duration(everyMS)*time.Millisecond, minSnapshotInterval), maxSnapshotInterval)
}

// maxRequest bounds a request line, so that a confused client cannot make
// the server buffer without end.
const maxRequest = 4096

// maxPreview bounds a request that carries the text of a profile.
const maxPreview = 1 << 20

// Server answers the clients of a running HIDI.
type Server struct {
	listener net.Listener
	path     string
}

// Listen creates the control socket. The directory is created for the user
// alone. A socket left behind by a HIDI that no longer runs is replaced;
// one that is answered means HIDI is running, and gives [ErrRunning].
func Listen(path string) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating the directory of the control socket: %w", err)
	}
	// Looking whether a HIDI answers, clearing away what one left behind
	// and taking the socket are three steps. Two HIDIs started at the same
	// moment could both find nobody answering and both go on; a lock on a
	// file beside the socket lets one of them do the three at a time. The
	// lock ends when the file is closed.
	if lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		defer lock.Close()
		if err := lockFile(lock); err != nil {
			return nil, fmt.Errorf("locking the control socket: %w", err)
		}
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
		return nil, ErrRunning
	}
	// Nobody answers: whatever is there is left over.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("removing the old control socket: %w", err)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("creating the control socket: %w", err)
	}
	// The directory already keeps others out; this covers a directory
	// that existed with wider permissions.
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, fmt.Errorf("restricting the control socket: %w", err)
	}
	return &Server{listener: l, path: path}, nil
}

// Close removes the socket without serving. It is for a HIDI that fails to
// start after it claimed the socket; [Server.Serve] closes by itself.
func (s *Server) Close() error { return s.listener.Close() }

// Path returns the path of the socket.
func (s *Server) Path() string { return s.path }

// Serve answers clients until ctx is cancelled, then removes the socket
// and returns once every connection is finished.
func (s *Server) Serve(ctx context.Context, host Host) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var conns sync.WaitGroup
	go func() {
		<-ctx.Done()
		s.listener.Close() // also removes the socket file
	}()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			// A failure to accept is momentary, such as too many open
			// files; do not spin on it.
			time.Sleep(100 * time.Millisecond)
			continue
		}
		conns.Go(func() {
			defer conn.Close()
			// Closing the connection is what ends a handler that waits
			// to read or write.
			stop := context.AfterFunc(ctx, func() { conn.Close() })
			defer stop()
			handle(ctx, conn, host)
		})
	}
	conns.Wait()
}

// handle answers the one request of a connection.
func handle(ctx context.Context, conn net.Conn, host Host) {
	out := json.NewEncoder(conn)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	// A request is a line of at most maxRequest bytes, except for the one
	// that carries a profile, which says so in its first bytes.
	reader := bufio.NewReaderSize(conn, maxRequest)
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) && bytes.HasPrefix(line, []byte(previewHead)) {
		var rest []byte
		start := bytes.Clone(line)
		rest, err = bufio.NewReaderSize(reader, maxPreview).ReadSlice('\n')
		line = append(start, rest...)
	}
	if err != nil {
		out.Encode(response{Error: "expected one request line of JSON"})
		return
	}
	conn.SetReadDeadline(time.Time{})
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		out.Encode(response{Error: "the request is not valid JSON: " + err.Error()})
		return
	}

	snapshot := func() error {
		s := host.Snapshot()
		return out.Encode(response{Snapshot: &s})
	}
	switch req.Op {
	case "status":
		snapshot()
	case "on":
		host.SetActive(true)
		snapshot()
	case "off":
		host.SetActive(false)
		snapshot()
	case "toggle":
		host.SetActive(!host.Snapshot().Active)
		snapshot()
	case "learn":
		if host.SetLearning == nil {
			out.Encode(response{Error: "this HIDI has no learning mode"})
			return
		}
		host.SetLearning(!host.Snapshot().Learning)
		snapshot()
	case "preview":
		if host.Preview == nil {
			out.Encode(response{Error: "this HIDI cannot try profiles"})
			return
		}
		if err := host.Preview(req.File, req.Text); err != nil {
			out.Encode(response{Error: err.Error()})
			return
		}
		snapshot()
	case "watch":
		watch(ctx, conn, out, host, req.Logs, snapshotPace(req.EveryMS))
	default:
		out.Encode(response{Error: fmt.Sprintf("unknown operation %q", req.Op)})
	}
}

// watch sends snapshots and log lines until the client goes away.
func watch(ctx context.Context, conn net.Conn, out *json.Encoder, host Host, logs int, pace time.Duration) {
	changes, stop := host.Changes.Listen()
	defer stop()

	// A client that closes the connection is noticed by reading from it.
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		var buf [1]byte
		for {
			if _, err := conn.Read(buf[:]); err != nil {
				return
			}
		}
	}()

	var nextLog int
	var logTick <-chan time.Time
	if logs > 0 && host.LogLines != nil && host.LogTotal != nil {
		nextLog = max(host.LogTotal()-logs, 0)
		ticker := time.NewTicker(logInterval)
		defer ticker.Stop()
		logTick = ticker.C
	}
	sendLogs := func() error {
		if logTick == nil {
			return nil
		}
		total := host.LogTotal()
		for _, line := range host.LogLines(nextLog, total) {
			if err := out.Encode(response{Log: &line}); err != nil {
				return err
			}
		}
		nextLog = total
		return nil
	}
	sendSnapshot := func() error {
		s := host.Snapshot()
		return out.Encode(response{Snapshot: &s})
	}

	if sendSnapshot() != nil || sendLogs() != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-gone:
			return
		case <-logTick:
			if sendLogs() != nil {
				return
			}
		case <-changes:
			if sendSnapshot() != nil {
				return
			}
			// Let further changes gather before the next snapshot.
			select {
			case <-time.After(pace):
			case <-ctx.Done():
				return
			case <-gone:
				return
			}
		}
	}
}
