package winmidi

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/gethiox/HIDI/internal/midi"
)

func TestHStringRoundTrip(t *testing.T) {
	if err := initRuntime(); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"", "HIDI Keyboard", "Zażółć 鍵盤 🎹"} {
		h, err := newHString(s)
		if err != nil {
			t.Fatalf("newHString(%q): %v", s, err)
		}
		if got := h.String(); got != s {
			t.Errorf("round trip of %q gave %q", s, got)
		}
		h.free()
	}
}

func TestHResultText(t *testing.T) {
	text := regdbEClassNotReg.Error()
	if !strings.Contains(text, "0x80040154") {
		t.Errorf("error text %q lacks the code", text)
	}
	if !regdbEClassNotReg.failed() || hresult(0).failed() || hresult(1).failed() {
		t.Error("failed() is wrong about success and failure codes")
	}
}

func TestGUIDArgs(t *testing.T) {
	g := guid("8087b303-0519-c0de-31d1-ee0010000000")
	args := guidArgs(&g)
	if n := len(args); n != 1 && n != 2 {
		t.Fatalf("%d arguments for a GUID", n)
	}
	if g.Data1 != 0x8087b303 || g.Data4[7] != 0 {
		t.Errorf("guid parsed to %+v", g)
	}
}

// Activating a class that every Windows has shows that the activation and
// the calling of methods work, independently of MIDI.
func TestActivationOfAKnownClass(t *testing.T) {
	iidStringable := guid("96369F54-8EB6-48F0-ABCE-C1B211E627C3")
	// Windows.Foundation.Uri has no constructor without parameters; its
	// factory is still an object to query.
	f, from, err := factory("Windows.Foundation.Uri", iidIActivationFactory)
	if err != nil {
		t.Fatalf("factory of Windows.Foundation.Uri: %v", err)
	}
	defer f.release()
	if from != sourceSystem {
		t.Errorf("found in %v, want Windows", from)
	}
	if _, err := f.query(iidStringable); err == nil {
		t.Error("the factory claims an interface it cannot have")
	}
	if _, _, err := factory("Hidi.Does.Not.Exist", iidIActivationFactory); err == nil {
		t.Error("a class that does not exist was activated")
	}
}

// On a Windows without MIDI Services, which the CI machines are, the
// package must say so and must not crash.
func TestCheckAndOpenReportWhatIsMissing(t *testing.T) {
	st, err := Check()
	t.Logf("status: %+v, error: %v", st, err)
	if st.Build == 0 {
		t.Error("the Windows build number was not read")
	}
	_, _, build := windows.RtlGetNtVersionNumbers()
	if st.Build != build&0xFFFF {
		t.Errorf("build %d, want %d", st.Build, build&0xFFFF)
	}
	if err == nil {
		// A machine with MIDI Services: the real thing can be tried.
		e, err := Open("HIDI unit test")
		if err != nil {
			t.Fatalf("Open on a machine with MIDI Services: %v", err)
		}
		if err := e.Send([]midi.Message{midi.NoteOn(0, 60, midi.From7(100)), midi.NoteOff(0, 60, 0)}); err != nil {
			t.Errorf("Send: %v", err)
		}
		if err := e.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		if err := e.Send(nil); err != ErrClosed {
			t.Errorf("Send after Close = %v", err)
		}
		return
	}
	if _, err := Open("HIDI unit test"); err == nil {
		t.Error("Open succeeded although Check failed")
	}
}

func TestInstanceID(t *testing.T) {
	for name, want := range map[string]string{
		"HIDI test device":               "HIDI-test-device",
		"HIDI VKBsim Gladiator EVO  R 2": "HIDI-VKBsim-Gladiator-EVO-R-2",
		"Keychron K2":                    "HIDI-Keychron-K2",
		"HIDI Pad (USB) #2":              "HIDI-Pad-USB-2",
		"":                               "HIDI",
	} {
		if got := instanceID(name); got != want {
			t.Errorf("instanceID(%q) = %q, want %q", name, got, want)
		}
	}
}

// Names in another alphabet keep identifiers of their own.
func TestInstanceIDOfForeignNames(t *testing.T) {
	a, b := instanceID("HIDI Клавиатура"), instanceID("HIDI Геймпад")
	if a == b || !strings.HasPrefix(a, "HIDI-") || len(a) != len("HIDI-")+8 {
		t.Errorf("identifiers %q and %q", a, b)
	}
	if again := instanceID("HIDI Клавиатура"); again != a {
		t.Errorf("the identifier changed: %q then %q", a, again)
	}
}

func TestResponseStatusNames(t *testing.T) {
	if responseStatus(808) != "error from the service" || responseStatus(0) != "success" || responseStatus(1) != "unknown" {
		t.Error("status names are wrong")
	}
}

// A stage that never returns must not hang its caller, and nothing may be
// attempted after it.
func TestStuckStageIsAbandoned(t *testing.T) {
	old := stepTimeout
	stepTimeout = 50 * time.Millisecond
	defer func() { stepTimeout = old }()

	var out strings.Builder
	tr := &tracer{w: &out}
	if err := tr.step("a quick stage", func() error { return nil }); err != nil {
		t.Fatalf("a quick stage failed: %v", err)
	}
	never := make(chan struct{})
	defer close(never)
	start := time.Now()
	err := tr.step("a stage that hangs", func() error { <-never; return nil })
	if !errors.Is(err, errStuck) {
		t.Fatalf("the hanging stage returned %v", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("waited %v for a stage limited to %v", waited, stepTimeout)
	}
	ran := false
	if err := tr.step("the next stage", func() error { ran = true; return nil }); !errors.Is(err, errStuck) || ran {
		t.Errorf("a stage ran after one got stuck: ran %v, error %v", ran, err)
	}
	if text := out.String(); !strings.Contains(text, "a quick stage ... ok") || !strings.Contains(text, "NO ANSWER") {
		t.Errorf("the trace reads:\n%s", text)
	}

	// An endpoint whose calls stopped returning neither sends nor waits.
	e := &Endpoint{trace: tr}
	if err := e.Send([]midi.Message{midi.NoteOn(0, 60, midi.From7(100))}); !errors.Is(err, errStuck) {
		t.Errorf("Send on a stuck endpoint = %v", err)
	}
	start = time.Now()
	if err := e.Close(); err == nil || time.Since(start) > time.Second {
		t.Errorf("Close on a stuck endpoint = %v after %v", err, time.Since(start))
	}
}
