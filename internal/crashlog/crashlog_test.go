package crashlog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The test runs itself as a second process that crashes, since a crash
// cannot be observed from inside the process it ends.
func TestCrashIsWrittenAndFoundAtTheNextStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hidi")
	if os.Getenv("HIDI_CRASH_TEST") == "1" {
		if _, err := Start(os.Getenv("HIDI_CRASH_DIR")); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { panic("the failure this test is about") }() // not the main goroutine: nothing can recover it
		<-done
		return
	}

	previous, err := Start(dir)
	if err != nil || previous != "" {
		t.Fatalf("first Start = %q, %v; want no earlier crash", previous, err)
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashIsWrittenAndFoundAtTheNextStart$")
	cmd.Env = append(os.Environ(), "HIDI_CRASH_TEST=1", "HIDI_CRASH_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the crashing process ended normally:\n%s", out)
	}
	written, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"the failure this test is about", "goroutine"} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the crash file lacks %q:\n%s", want, written)
		}
	}

	// The next start finds it, keeps it under another name, and starts
	// with an empty file again.
	previous, err = Start(dir)
	if err != nil || previous == "" {
		t.Fatalf("Start after a crash = %q, %v; want the kept report", previous, err)
	}
	kept, err := os.ReadFile(previous)
	if err != nil || string(kept) != string(written) {
		t.Errorf("the kept report differs from what was written: %v", err)
	}
	if info, err := os.Stat(Path(dir)); err != nil || info.Size() != 0 {
		t.Errorf("the new crash file is not empty: %v", err)
	}
	if again, err := Start(dir); err != nil || again != "" {
		t.Errorf("Start after a clean run = %q, %v", again, err)
	}
}

func TestReport(t *testing.T) {
	dir := t.TempDir()
	text := Report("index out of range", dir)
	for _, want := range []string{"bug in it: index out of range", Path(dir)} {
		if !strings.Contains(text, want) {
			t.Errorf("report %q lacks %q", text, want)
		}
	}
	written, err := os.ReadFile(Path(dir))
	if err != nil || !strings.Contains(string(written), "index out of range") || !strings.Contains(string(written), "TestReport") {
		t.Errorf("the recovered failure was not written with its stack: %v\n%s", err, written)
	}
}
