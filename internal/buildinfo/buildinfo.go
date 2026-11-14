// Package buildinfo exposes the version of the running binary.
//
// Release builds set Version with the linker:
//
//	go build -ldflags "-X github.com/gethiox/HIDI/internal/buildinfo.Version=2.0.0" ./cmd/hidi
//
// Development builds leave it unset and fall back to the VCS revision that
// the Go toolchain embeds in the binary.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version is the release version, set at link time. It is empty for
// development builds.
var Version string

// Info describes the running binary.
type Info struct {
	// Version is the release version, or "dev" for development builds.
	Version string
	// Revision is the VCS commit the binary was built from, if known.
	Revision string
	// Modified reports whether the working tree had uncommitted changes.
	Modified bool
	// GoVersion is the Go toolchain version used for the build.
	GoVersion string
	// OS and Arch are the build target.
	OS, Arch string
}

// Get returns information about the running binary.
func Get() Info {
	bi, _ := debug.ReadBuildInfo()
	return fromBuildInfo(Version, bi)
}

func fromBuildInfo(version string, bi *debug.BuildInfo) Info {
	info := Info{
		Version:   version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	if bi == nil {
		return info
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	return info
}

// String formats the information on one line, for example
// "hidi 2.0.0 (1a2b3c4d5e6f) go1.24.7 linux/amd64".
func (i Info) String() string {
	rev := ""
	if i.Revision != "" {
		short := i.Revision
		if len(short) > 12 {
			short = short[:12]
		}
		if i.Modified {
			short += "+dirty"
		}
		rev = " (" + short + ")"
	}
	return fmt.Sprintf("hidi %s%s %s %s/%s", i.Version, rev, i.GoVersion, i.OS, i.Arch)
}
