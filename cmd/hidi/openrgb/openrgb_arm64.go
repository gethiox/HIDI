//go:build linux && arm64 && openrgb

package openrgb

import _ "embed"

//go:embed bin/openrgb-arm64
var OpenRGB []byte
var OpenRGBVersion = "arm64"
