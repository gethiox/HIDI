// Package tray shows HIDI in the notification area of the desktop: an icon
// that tells whether HIDI is on, and a menu to turn it on and off.
//
// The icon is a client of the running HIDI, like the terminal views. It
// can be started before HIDI and outlives it; it then shows that HIDI is
// not running and waits for it.
//
// The part that talks to the desktop is a [Host], one per system:
//
//   - Linux: the StatusNotifierItem protocol over D-Bus, with the menu as a
//     com.canonical.dbusmenu object, which is what KDE, GNOME with its
//     AppIndicator extension, Xfce and the bars of Wayland compositors
//     understand.
//   - Windows: Shell_NotifyIcon.
//
// The icon is drawn by the program, so no image files are installed.
package tray
