package main

// The Windows program carries HIDI's icon and its version as resources.
// They are made from the drawing of the icon, not kept in the repository:
//
//	go generate ./cmd/hidi
//
// draws the icon in the sizes Windows asks for and packs them into files
// "go build" links into hidi.exe by itself. Without them the program is
// the same, with the icon Windows gives a program that has none. A
// release is built with them, see scripts/release.sh.

//go:generate go run ../../internal/assets/icon/render winres
//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64,arm64
