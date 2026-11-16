//go:build !linux && !windows

package tray

import "errors"

// NewHost reports that this system has no notification area HIDI knows.
func NewHost() (Host, error) {
	return nil, errors.New("HIDI has no icon for the notification area of this system")
}
