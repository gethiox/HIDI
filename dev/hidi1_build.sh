#!/usr/bin/bash

ssh pi@hidi 'fish -c "cd HIDI; go run build.go -cgo -openrgb -platforms linux-arm-v6,linux-arm-v7"'