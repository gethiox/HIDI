// Package logging is HIDI's log: levelled lines from named components with
// structured values, written compact and optionally coloured to a terminal,
// and kept in memory for the interfaces to show.
//
// It is a [log/slog] handler, so components log through a [*slog.Logger]:
//
//	log := logging.New(logging.Options{Output: os.Stderr})
//	log.Logger("input").Info("device connected", "name", name)
//
// A line consists of the date, the time with milliseconds, the level, the
// component in square brackets, the message and the values:
//
//	2026-11-16 21:04:11.482  INFO [input] device connected name='Keychron K2'
//
// Every part has a colour of its own; a component's colour is derived from
// its name, so it is the same in every run. Values are coloured by kind.
//
// Text that comes from outside HIDI, such as the name a device reports, is
// written with its control characters escaped, so that it can neither forge
// a log line nor send escape sequences to the terminal.
package logging
