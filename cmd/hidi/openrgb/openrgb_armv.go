//go:build linux && arm && openrgb

package openrgb

import _ "embed"

//go:embed bin/openrgb-arm-v6
var OpenRGB []byte
var OpenRGBArchitecture = "arm-v6"
var OpenRGBVersion = "0.9"
