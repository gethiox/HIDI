// Package ipc is the local control interface of a running HIDI: how
// "hidi status", "hidi off" and the interfaces talk to "hidi run".
//
// The running HIDI listens on a Unix domain socket in a directory only the
// user can enter. Windows has such sockets too, so the same code serves
// both systems.
//
// The protocol is one JSON object per line in each direction. A client
// sends one request per connection:
//
//	{"op":"status"}            answered with one snapshot
//	{"op":"on"}                turn HIDI on; answered with a snapshot
//	{"op":"off"}               turn HIDI off; answered with a snapshot
//	{"op":"toggle"}            the other state; answered with a snapshot
//	{"op":"watch","logs":20}   a snapshot now and after every change, and
//	                           with "logs" the last so many log lines
//	                           followed by every new one, until the
//	                           client closes the connection
//
// Every answer is an object with one of "snapshot", "log" or "error" set.
// Fields may be added to the objects; a reader ignores what it does not
// know.
package ipc
