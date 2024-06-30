//go:build !openrgb

package openrgb

import _ "embed"

var OpenRGB []byte
var OpenRGBArchitecture = "N/A"
var OpenRGBVersion = "N/A"
