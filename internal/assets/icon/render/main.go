// Command render draws HIDI's icon into PNG files, for what wants the
// icon as a picture: the resources of the Windows program, and a desktop
// entry on Linux.
//
//	go run ./internal/assets/icon/render DIRECTORY [SIZE...]
//
// writes hidi-SIZE.png for every size into the directory; without sizes,
// those an icon of a Windows program has.
package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gethiox/HIDI/internal/assets/icon"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: render DIRECTORY [SIZE...]")
		os.Exit(2)
	}
	dir := os.Args[1]
	sizes := []int{256, 64, 48, 32, 24, 16}
	if len(os.Args) > 2 {
		sizes = nil
		for _, arg := range os.Args[2:] {
			size, err := strconv.Atoi(arg)
			if err != nil || size < 1 || size > 4096 {
				fmt.Fprintf(os.Stderr, "render: %q is not a size\n", arg)
				os.Exit(2)
			}
			sizes = append(sizes, size)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
	for _, size := range sizes {
		if err := write(filepath.Join(dir, fmt.Sprintf("hidi-%d.png", size)), size); err != nil {
			fmt.Fprintln(os.Stderr, "render:", err)
			os.Exit(1)
		}
	}
}

func write(file string, size int) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	// The same bytes for the same picture, whoever draws it.
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(f, icon.Render(size, icon.Program)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
