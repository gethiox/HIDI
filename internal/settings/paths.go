package settings

import (
	"fmt"
	"os"
	"path/filepath"
)

// DirName is the name of HIDI's directory in the system's place for
// configuration.
const DirName = "HIDI"

// Paths says where HIDI's files are.
type Paths struct {
	// Root is HIDI's directory.
	Root string
	// Settings is the file with HIDI's own settings.
	Settings string
	// Profiles is the directory of the profiles.
	Profiles string
	// Seen is the file in which HIDI notes the devices it has met.
	Seen string
	// record is the file in which HIDI notes what it wrote.
	record string
}

// DefaultRoot returns HIDI's directory for the user: HIDI in the user's
// configuration directory.
func DefaultRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the configuration directory: %w", err)
	}
	return filepath.Join(base, DirName), nil
}

// PathsIn returns where the files are inside a root directory.
func PathsIn(root string) Paths {
	return Paths{
		Root:     root,
		Settings: filepath.Join(root, "settings.toml"),
		Profiles: filepath.Join(root, "profiles"),
		Seen:     filepath.Join(root, ".devices.json"),
		record:   filepath.Join(root, ".written.json"),
	}
}
