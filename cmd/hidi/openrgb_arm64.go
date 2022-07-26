//go:build linux && arm64 && openrgb

package main

import _ "embed"

//go:embed OpenRGB/openrgb-arm64
var OpenRGB []byte
var OpenRGBVersion = "arm64"
