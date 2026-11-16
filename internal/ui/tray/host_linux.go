package tray

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// Names from the StatusNotifierItem and dbusmenu protocols.
const (
	watcherName  = "org.kde.StatusNotifierWatcher"
	watcherPath  = "/StatusNotifierWatcher"
	itemIface    = "org.kde.StatusNotifierItem"
	itemPath     = "/StatusNotifierItem"
	menuIface    = "com.canonical.dbusmenu"
	menuPath     = "/MenuBar"
	busName      = "org.freedesktop.DBus"
	nameOwnerSig = busName + ".NameOwnerChanged"
)

// iconSizes are the sizes the icon is offered in; the desktop takes the
// one that fits its panel.
var iconSizes = []int{16, 22, 24, 32, 48, 64}

// pixmap is an image as the protocol wants it: the pixels as alpha, red,
// green, blue bytes.
type pixmap struct {
	Width, Height int32
	Data          []byte
}

func pixmaps(look Look) []pixmap {
	var out []pixmap
	for _, size := range iconSizes {
		img := Icon(look, size)
		data := make([]byte, 0, size*size*4)
		for i := 0; i < len(img.Pix); i += 4 {
			data = append(data, img.Pix[i+3], img.Pix[i], img.Pix[i+1], img.Pix[i+2])
		}
		out = append(out, pixmap{int32(size), int32(size), data})
	}
	return out
}

// tooltip is the protocol's structure for the text under the pointer.
type tooltip struct {
	IconName string
	Icon     []pixmap
	Title    string
	Text     string
}

// menuItem is one line of the menu as the desktop is told about it.
type menuItem struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

// Identifiers of the menu entries that can be chosen. The lines that only
// inform are numbered from firstLine.
const (
	idToggle  = 1
	idQuit    = 2
	firstLine = 100
)

type linuxHost struct {
	conn    *dbus.Conn
	name    string
	props   *prop.Properties
	actions chan Action
	signals chan *dbus.Signal
	done    chan struct{}
	close   sync.Once

	mu       sync.Mutex
	view     View
	revision uint32
}

// NewHost puts an icon into the notification area. The icon appears when
// the desktop has such an area, also when that starts later.
func NewHost() (Host, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connecting to the desktop session (D-Bus): %w", err)
	}
	h := &linuxHost{
		conn:    conn,
		name:    fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid()),
		actions: make(chan Action, 4),
		signals: make(chan *dbus.Signal, 16),
		done:    make(chan struct{}),
		view:    ViewOf(nil),
	}
	if err := h.export(); err != nil {
		conn.Close()
		return nil, err
	}
	// The area may start after the icon, or start again: register with
	// every one that appears.
	conn.Signal(h.signals)
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface(busName), dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherName),
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("watching for the notification area: %w", err)
	}
	go h.watch()
	h.register()
	return h, nil
}

func (h *linuxHost) export() error {
	reply, err := h.conn.RequestName(h.name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("taking a name in the desktop session: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return errors.New("the name of the icon in the desktop session is taken")
	}

	constant := func(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitFalse} }
	h.props, err = prop.Export(h.conn, itemPath, prop.Map{
		itemIface: {
			"Category":   constant("ApplicationStatus"),
			"Id":         constant("hidi"),
			"Title":      constant("HIDI"),
			"Status":     constant("Active"),
			"WindowId":   constant(uint32(0)),
			"IconName":   constant(""),
			"IconPixmap": constant(pixmaps(h.view.Look)),
			"ToolTip":    constant(tooltip{Title: h.view.Title, Icon: []pixmap{}}),
			"ItemIsMenu": constant(false),
			"Menu":       constant(dbus.ObjectPath(menuPath)),
		},
	})
	if err != nil {
		return fmt.Errorf("offering the icon: %w", err)
	}
	if err := h.conn.Export(itemMethods{h}, itemPath, itemIface); err != nil {
		return fmt.Errorf("offering the icon: %w", err)
	}
	menuProps, err := prop.Export(h.conn, menuPath, prop.Map{
		menuIface: {
			"Version":       constant(uint32(3)),
			"Status":        constant("normal"),
			"TextDirection": constant("ltr"),
			"IconThemePath": constant([]string{}),
		},
	})
	if err != nil {
		return fmt.Errorf("offering the menu: %w", err)
	}
	if err := h.conn.Export(menuMethods{h}, menuPath, menuIface); err != nil {
		return fmt.Errorf("offering the menu: %w", err)
	}

	// Desktops ask what the objects can do before they use them.
	item := &introspect.Node{Name: itemPath, Interfaces: []introspect.Interface{
		introspect.IntrospectData, prop.IntrospectData,
		{
			Name:       itemIface,
			Methods:    introspect.Methods(itemMethods{h}),
			Properties: h.props.Introspection(itemIface),
			Signals: []introspect.Signal{
				{Name: "NewIcon"}, {Name: "NewToolTip"}, {Name: "NewTitle"},
				{Name: "NewStatus", Args: []introspect.Arg{{Name: "status", Type: "s"}}},
			},
		},
	}}
	menu := &introspect.Node{Name: menuPath, Interfaces: []introspect.Interface{
		introspect.IntrospectData, prop.IntrospectData,
		{
			Name:       menuIface,
			Methods:    introspect.Methods(menuMethods{h}),
			Properties: menuProps.Introspection(menuIface),
			Signals: []introspect.Signal{
				{Name: "LayoutUpdated", Args: []introspect.Arg{{Name: "revision", Type: "u"}, {Name: "parent", Type: "i"}}},
				{Name: "ItemsPropertiesUpdated", Args: []introspect.Arg{{Name: "updated", Type: "a(ia{sv})"}, {Name: "removed", Type: "a(ias)"}}},
			},
		},
	}}
	if err := h.conn.Export(introspect.NewIntrospectable(item), itemPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	return h.conn.Export(introspect.NewIntrospectable(menu), menuPath, "org.freedesktop.DBus.Introspectable")
}

// register tells the notification area about the icon. Without an area
// there is nobody to tell, which is not an error: watch tries again when
// one appears.
func (h *linuxHost) register() {
	h.conn.Object(watcherName, watcherPath).Call(watcherName+".RegisterStatusNotifierItem", 0, h.name)
}

func (h *linuxHost) watch() {
	for {
		select {
		case <-h.done:
			return
		case sig, ok := <-h.signals:
			if !ok {
				return
			}
			// NameOwnerChanged(name, old owner, new owner)
			if sig.Name == nameOwnerSig && len(sig.Body) == 3 {
				if owner, _ := sig.Body[2].(string); owner != "" {
					h.register()
				}
			}
		}
	}
}

func (h *linuxHost) Show(v View) error {
	h.mu.Lock()
	old := h.view
	h.view = v
	h.revision++
	revision := h.revision
	h.mu.Unlock()

	if v.Look != old.Look {
		h.props.SetMust(itemIface, "IconPixmap", pixmaps(v.Look))
		if err := h.conn.Emit(itemPath, itemIface+".NewIcon"); err != nil {
			return err
		}
	}
	if v.Title != old.Title {
		h.props.SetMust(itemIface, "ToolTip", tooltip{Title: v.Title, Icon: []pixmap{}})
		if err := h.conn.Emit(itemPath, itemIface+".NewToolTip"); err != nil {
			return err
		}
	}
	return h.conn.Emit(menuPath, menuIface+".LayoutUpdated", revision, int32(0))
}

func (h *linuxHost) Actions() <-chan Action { return h.actions }

func (h *linuxHost) Close() error {
	h.close.Do(func() {
		close(h.done)
		h.conn.Close()
	})
	return nil
}

func (h *linuxHost) act(a Action) {
	select {
	case h.actions <- a:
	default: // the user is clicking faster than HIDI answers
	}
}

// layout builds the menu from the view.
func (h *linuxHost) layout() (uint32, menuItem) {
	h.mu.Lock()
	v, revision := h.view, h.revision
	h.mu.Unlock()

	item := func(id int32, props map[string]any) dbus.Variant {
		m := map[string]dbus.Variant{}
		for k, val := range props {
			m[k] = dbus.MakeVariant(val)
		}
		return dbus.MakeVariant(menuItem{ID: id, Props: m, Children: []dbus.Variant{}})
	}
	separator := func(id int32) dbus.Variant { return item(id, map[string]any{"type": "separator"}) }

	var children []dbus.Variant
	for i, line := range v.Lines {
		children = append(children, item(firstLine+int32(i), map[string]any{"label": line, "enabled": false}))
	}
	if v.Toggle != "" {
		children = append(children, separator(3), item(idToggle, map[string]any{"label": v.Toggle}))
	}
	children = append(children, separator(4), item(idQuit, map[string]any{"label": QuitLabel}))
	root := menuItem{ID: 0, Props: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}, Children: children}
	return revision, root
}

// itemMethods are the calls a desktop makes on the icon.
type itemMethods struct{ h *linuxHost }

// Activate is a click on the icon.
func (m itemMethods) Activate(x, y int32) *dbus.Error {
	m.h.act(ActionToggle)
	return nil
}

// SecondaryActivate is a click with the middle button.
func (m itemMethods) SecondaryActivate(x, y int32) *dbus.Error {
	m.h.act(ActionToggle)
	return nil
}

// ContextMenu asks the icon to show a menu of its own; the desktop shows
// the one it was given instead.
func (m itemMethods) ContextMenu(x, y int32) *dbus.Error { return nil }

// Scroll is the wheel turned over the icon.
func (m itemMethods) Scroll(delta int32, orientation string) *dbus.Error { return nil }

// menuMethods are the calls a desktop makes on the menu.
type menuMethods struct{ h *linuxHost }

func (m menuMethods) GetLayout(parent, depth int32, names []string) (uint32, menuItem, *dbus.Error) {
	revision, root := m.h.layout()
	if parent != 0 {
		// The entries have no entries of their own.
		for _, child := range root.Children {
			if c := child.Value().(menuItem); c.ID == parent {
				return revision, c, nil
			}
		}
		return 0, menuItem{}, dbus.MakeFailedError(fmt.Errorf("the menu has no entry %d", parent))
	}
	return revision, root, nil
}

// menuProps is an entry and its properties, for GetGroupProperties.
type menuProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

func (m menuMethods) GetGroupProperties(ids []int32, names []string) ([]menuProps, *dbus.Error) {
	_, root := m.h.layout()
	all := append([]dbus.Variant{dbus.MakeVariant(root)}, root.Children...)
	var out []menuProps
	for _, v := range all {
		item := v.Value().(menuItem)
		wanted := len(ids) == 0
		for _, id := range ids {
			wanted = wanted || id == item.ID
		}
		if wanted {
			out = append(out, menuProps{item.ID, item.Props})
		}
	}
	return out, nil
}

func (m menuMethods) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	props, _ := m.GetGroupProperties([]int32{id}, nil)
	if len(props) == 1 {
		if v, ok := props[0].Props[name]; ok {
			return v, nil
		}
	}
	return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("entry %d has no property %q", id, name))
}

// Event tells what the user did with an entry.
func (m menuMethods) Event(id int32, event string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if event != "clicked" {
		return nil
	}
	switch id {
	case idToggle:
		m.h.act(ActionToggle)
	case idQuit:
		m.h.act(ActionQuit)
	}
	return nil
}

// menuEvent is one event of EventGroup.
type menuEvent struct {
	ID        int32
	Event     string
	Data      dbus.Variant
	Timestamp uint32
}

func (m menuMethods) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	for _, e := range events {
		m.Event(e.ID, e.Event, e.Data, e.Timestamp)
	}
	return []int32{}, nil
}

// AboutToShow lets a menu prepare itself; this one is always ready.
func (m menuMethods) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

func (m menuMethods) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}
