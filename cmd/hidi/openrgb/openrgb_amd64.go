//go:build linux && amd64 && openrgb

package openrgb

import _ "embed"

//go:embed bin/openrgb-amd64
var OpenRGB []byte
var OpenRGBArchitecture = "amd64"
var OpenRGBVersion = "0.9"
