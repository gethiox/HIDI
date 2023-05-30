//go:build arm || arm64

package gyro

import (
	"context"
	"fmt"

	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/kidoman/embd"
	_ "github.com/kidoman/embd/host/rpi"
)

var log = logger.GetLogger()

func getCalibrationData(bus embd.I2CBus, addr byte, samples int) (float64, float64, float64, error) {
	var ox, oy, oz float64

	for i := 0; i < samples; i++ {
		x, y, z, err := readGyro(bus, addr)
		if err != nil {
			return ox, oy, oz, err
		}
		ox += x / float64(samples)
		oy += y / float64(samples)
		oz += z / float64(samples)
	}

	return ox, oy, oz, nil
}

func readGyro(bus embd.I2CBus, addr byte) (float64, float64, float64, error) {
	var data = make([]byte, 6)
	err := bus.ReadFromReg(addr, 0x43, data)

	var x, y, z int16

	x = int16(data[0])<<8 + int16(data[1])
	y = int16(data[2])<<8 + int16(data[3])
	z = int16(data[4])<<8 + int16(data[5])

	return float64(x) / 10000, float64(y) / 10000, float64(z) / 10000, err
}

type Vector struct {
	X, Y, Z float64
}

func (v Vector) String() string {
	return fmt.Sprintf("x: %5.2f, y: %5.2f, z: %5.2f", v.X, v.Y, v.Z)
}

func ProcessGyro(ctx context.Context, address, bus byte) (chan Vector, error) {
	log.Info("[Gyro source] Process Gyro engaged", logger.Debug)
	i2c := embd.NewI2CBus(bus)

	err := i2c.WriteToReg(address, 0x6B, []byte{0})
	if err != nil {
		return nil, fmt.Errorf("failed to initiate device: %w", err)
	}

	fs_sel := uint8(3)
	err = i2c.WriteToReg(address, 0x1b, []byte{fs_sel << 3}) // set resolution
	if err != nil {
		return nil, fmt.Errorf("failed to set resolution: %w", err)
	}

	data := make(chan Vector, 10)

	log.Info("[Gyro source] starting goroutine", logger.Debug)
	go func() {
		defer i2c.Close()
		defer close(data)

		log.Info("[Gyro source] calculating offset, keep device on the ground", logger.Info)
		ox, oy, oz, err := getCalibrationData(i2c, address, 3000)
		if err != nil {
			return
		}
		log.Info(fmt.Sprintf(
			"[Gyro source] calculating offset done (x: %.2f, y: %.2f, z: %.2f), ready to go",
			ox, oy, oz,
		), logger.Info)

	root:
		for {
			select {
			case <-ctx.Done():
				break root
			default:
				break
			}

			x, y, z, err := readGyro(i2c, address)
			if err != nil {
				return
			}

			data <- Vector{
				X: x - ox,
				Y: y - oy,
				Z: z - oz,
			}
		}
		log.Info("[Gyro source] gyro goroutine exited", logger.Debug)
	}()

	log.Info("[Gyro source] Process Gyro returning channel", logger.Debug)
	return data, nil
}
