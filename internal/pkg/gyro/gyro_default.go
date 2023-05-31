//go:build !arm && !arm64

package gyro

import (
	"context"
	"errors"
	"fmt"
)

type Vector struct {
	X, Y, Z float64
}

func (v Vector) String() string {
	return fmt.Sprintf("x: %5.2f, y: %5.2f, z: %5.2f", v.X, v.Y, v.Z)
}

func ProcessGyro(ctx context.Context, address, bus byte) (chan Vector, error) {
	return nil, errors.New("hardware not supported")
}
