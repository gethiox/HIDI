//go:build linux && arm64 && openrgb

package openrgb

import _ "embed"

//go:embed bin/openrgb-arm64
var OpenRGB []byte
var OpenRGBArchitecture = "arm64"
var OpenRGBVersion = "0.9"
