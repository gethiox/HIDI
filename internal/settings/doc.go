// Package settings holds HIDI's own settings and knows where HIDI keeps
// its files.
//
// Everything the user can change lives in one directory, named HIDI, in
// the place the system has for configuration: ~/.config/HIDI on Linux,
// %AppData%\HIDI on Windows.
//
//	settings.toml   HIDI's own settings: this package
//	profiles/       one file for each profile: package config
//
// The settings are kept apart from the profiles: a profile says how one
// kind of device plays, the settings say how HIDI itself behaves.
//
// On its first start HIDI fills the directory: it writes the settings
// with their defaults and the profiles it comes with, as files to change.
// From then on the directory is what counts. A later HIDI brings a
// built-in profile up to date only if the user has not changed the file,
// and adds profiles that are new. A directory HIDI does not recognise,
// one it did not make, is left untouched.
package settings
