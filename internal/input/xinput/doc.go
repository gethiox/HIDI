// Package xinput reads Xbox controllers, and the many controllers that
// imitate one, on Windows through XInput.
//
// Windows presents such a controller twice: through XInput, complete, and
// as a plain HID device for older programs, on which the two triggers
// share one axis. So HIDI reads these controllers here and leaves the HID
// side of them alone; package rawinput reads every other controller.
//
// XInput has no notifications. It knows up to four controllers by number
// and is asked for their state: often for those that are connected, and
// once a second whether a new one appeared.
//
// The controls get the codes the Linux kernel gives an Xbox controller.
//
// The files without a build constraint hold the parts that do not call
// Windows, so that their tests run on every system.
package xinput
