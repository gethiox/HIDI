package tray

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// sessionBus starts a D-Bus session of its own for the test.
func sessionBus(t *testing.T) {
	t.Helper()
	path, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	cmd := exec.Command(path, "--session", "--nofork", "--print-address")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("dbus-daemon does not start: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	address, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the address of the bus: %v", err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(address))
}

// area is a notification area as far as an icon talks to it.
type area struct{ registered chan string }

func (a area) RegisterStatusNotifierItem(service string) *dbus.Error {
	a.registered <- service
	return nil
}

func startArea(t *testing.T) (*dbus.Conn, area) {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	a := area{registered: make(chan string, 4)}
	if err := conn.Export(a, watcherPath, watcherName); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("taking the name of the area: %v, %v", reply, err)
	}
	return conn, a
}

func (a area) expect(t *testing.T) string {
	t.Helper()
	select {
	case name := <-a.registered:
		return name
	case <-time.After(5 * time.Second):
		t.Fatal("the icon did not register with the notification area")
		return ""
	}
}

func labels(root menuItem) []string {
	var out []string
	for _, child := range root.Children {
		var item menuItem
		if err := dbus.Store([]any{child.Value()}, &item); err != nil {
			panic(err)
		}
		switch {
		case item.Props["type"].Value() == "separator":
			out = append(out, "---")
		case item.Props["enabled"].Value() == false:
			out = append(out, "("+item.Props["label"].Value().(string)+")")
		default:
			out = append(out, item.Props["label"].Value().(string))
		}
	}
	return out
}

func TestLinuxHost(t *testing.T) {
	sessionBus(t)

	// The icon starts before the notification area and registers when
	// that appears.
	host, err := NewHost()
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	defer host.Close()
	conn, a := startArea(t)
	name := a.expect(t)

	item := conn.Object(name, itemPath)
	menu := conn.Object(name, menuPath)
	getLayout := func() (uint32, menuItem) {
		var revision uint32
		var root menuItem
		if err := menu.Call(menuIface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&revision, &root); err != nil {
			t.Fatalf("GetLayout: %v", err)
		}
		return revision, root
	}

	// What a desktop reads before it draws the icon.
	var icons []pixmap
	if err := item.StoreProperty(itemIface+".IconPixmap", &icons); err != nil {
		t.Fatalf("IconPixmap: %v", err)
	}
	if len(icons) != len(iconSizes) || int(icons[0].Width) != iconSizes[0] || len(icons[0].Data) != iconSizes[0]*iconSizes[0]*4 {
		t.Errorf("the icon comes in %d sizes, the first %dx%d with %d bytes", len(icons), icons[0].Width, icons[0].Height, len(icons[0].Data))
	}
	var menuAt dbus.ObjectPath
	if err := item.StoreProperty(itemIface+".Menu", &menuAt); err != nil || menuAt != menuPath {
		t.Errorf("Menu = %q, %v", menuAt, err)
	}
	var description string
	if err := item.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&description); err != nil || !strings.Contains(description, "Activate") {
		t.Errorf("the icon does not describe itself: %v", err)
	}

	_, root := getLayout()
	if got, want := strings.Join(labels(root), "|"), "(HIDI is not running)|---|"+QuitLabel; got != want {
		t.Errorf("menu without HIDI: %s, want %s", got, want)
	}

	// A change is announced and shows in what the desktop reads next.
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	if err := conn.AddMatchSignal(dbus.WithMatchSender(name)); err != nil {
		t.Fatal(err)
	}
	if err := host.Show(View{Look: LookOn, Title: "HIDI is on", Lines: []string{"HIDI is on", "Keyboard (channel 1)"}, Toggle: "Turn HIDI off"}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	seen := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for !(seen[itemIface+".NewIcon"] && seen[itemIface+".NewToolTip"] && seen[menuIface+".LayoutUpdated"]) {
		select {
		case sig := <-signals:
			seen[sig.Name] = true
		case <-deadline:
			t.Fatalf("signals after a change: %v", seen)
		}
	}
	var tip tooltip
	if err := item.StoreProperty(itemIface+".ToolTip", &tip); err != nil || tip.Title != "HIDI is on" {
		t.Errorf("ToolTip = %+v, %v", tip, err)
	}
	revision, root := getLayout()
	if got, want := strings.Join(labels(root), "|"), "(HIDI is on)|(Keyboard (channel 1))|---|Turn HIDI off|---|"+QuitLabel; got != want {
		t.Errorf("menu with HIDI: %s, want %s", got, want)
	}
	if revision == 0 {
		t.Error("the menu did not count its change")
	}

	// Clicks.
	expect := func(want Action) {
		t.Helper()
		select {
		case got := <-host.Actions():
			if got != want {
				t.Errorf("action %d, want %d", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no action %d", want)
		}
	}
	if err := item.Call(itemIface+".Activate", 0, int32(10), int32(10)).Err; err != nil {
		t.Fatalf("Activate: %v", err)
	}
	expect(ActionToggle)
	click := func(id int32) {
		t.Helper()
		if err := menu.Call(menuIface+".Event", 0, id, "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
			t.Fatalf("Event: %v", err)
		}
	}
	click(idToggle)
	expect(ActionToggle)
	click(idQuit)
	expect(ActionQuit)
	var needsUpdate bool
	if err := menu.Call(menuIface+".AboutToShow", 0, int32(0)).Store(&needsUpdate); err != nil {
		t.Errorf("AboutToShow: %v", err)
	}
	var props []menuProps
	if err := menu.Call(menuIface+".GetGroupProperties", 0, []int32{idToggle}, []string{}).Store(&props); err != nil || len(props) != 1 || props[0].Props["label"].Value() != "Turn HIDI off" {
		t.Errorf("GetGroupProperties = %+v, %v", props, err)
	}
}

// A notification area that starts again gets the icon again.
func TestLinuxHostRegistersAgain(t *testing.T) {
	sessionBus(t)
	conn, a := startArea(t)
	host, err := NewHost()
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	defer host.Close()
	first := a.expect(t)

	conn.Close()
	_, again := startArea(t)
	if second := again.expect(t); second != first {
		t.Errorf("registered as %q, then as %q", first, second)
	}
}
