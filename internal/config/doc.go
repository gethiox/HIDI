// Package config reads instrument profiles from TOML files.
//
// A profile file describes one instrument: which devices it applies to, its
// initial state, and its bindings. The format is documented for users in
// docs/configuration.md; [Parse] is the authoritative implementation.
//
// # Versioning
//
// Every file states the schema version it was written for. [Parse] accepts
// the current version and every older one, upgrading older files in memory
// one version at a time before reading them. A change to the format that
// would alter the meaning of existing files must increase [Schema] and add
// the corresponding step to the migrations table.
package config
