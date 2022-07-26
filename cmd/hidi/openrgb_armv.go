//go:build linux && arm && openrgb

package main

import _ "embed"

//go:embed OpenRGB/openrgb-arm-v6
var OpenRGB []byte
var OpenRGBVersion = "arm-v6"
