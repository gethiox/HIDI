module github.com/gethiox/HIDI

go 1.27.0

toolchain go1.27.4

require (
	github.com/gen2brain/raylib-go/raylib v0.63.0
	github.com/godbus/dbus/v5 v5.2.4
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/spf13/pflag v1.0.10
	golang.org/x/sys v0.50.0
)

require (
	github.com/ebitengine/purego v0.10.2 // indirect
	github.com/jupiterrider/ffi v0.8.0 // indirect
)

replace github.com/gen2brain/raylib-go/raylib => github.com/gethiox/raylib-go/raylib v0.0.0-20261120175209-db320c4ca904
