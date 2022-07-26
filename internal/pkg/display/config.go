package display

import "github.com/d2r2/go-hd44780"

type ScreenConfig struct {
	Enabled     bool
	LcdType     hd44780.LcdType
	Bus         int
	Address     uint8
	UpdateRate  int
	ExitMessage [4]string
}

func (s *ScreenConfig) HaveExitMessage() bool {
	for _, v := range s.ExitMessage {
		if len(v) > 0 {
			return true
		}
	}
	return false
}
