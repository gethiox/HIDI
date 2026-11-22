package tray

import (
	"image"

	"github.com/gethiox/HIDI/internal/assets/icon"
)

// Icon draws the icon for a look, size pixels wide and high: HIDI's own
// icon in the colours of the state. While HIDI is on, the letter in the
// keys is green; in the other half of the blinking of learning mode it is
// yellow. While HIDI is off the keys are grey and show no letter, and for
// a HIDI that is not running the whole icon is faint as well.
func Icon(look Look, size int) *image.NRGBA {
	switch look {
	case LookOn:
		return icon.Render(size, icon.On)
	case LookLearning:
		return icon.Render(size, icon.Learning)
	case LookStopped:
		return icon.Fade(icon.Render(size, icon.Off), 0.45)
	}
	return icon.Render(size, icon.Off)
}
