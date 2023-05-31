//go:build linux && amd64 && openrgb

package openrgb

import _ "embed"

//go:embed bin/openrgb-amd64
var OpenRGB []byte
var OpenRGBVersion = "amd64"
