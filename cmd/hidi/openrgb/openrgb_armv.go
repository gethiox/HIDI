//go:build linux && arm && openrgb

package openrgb

import _ "embed"

//go:embed bin/openrgb-arm-v6
var OpenRGB []byte
var OpenRGBVersion = "arm-v6"
