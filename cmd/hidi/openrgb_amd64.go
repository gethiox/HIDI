//go:build linux && amd64 && openrgb

package main

import _ "embed"

//go:embed OpenRGB/openrgb-amd64
var OpenRGB []byte
var OpenRGBVersion = "amd64"
