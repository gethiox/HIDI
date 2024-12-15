#!/usr/bin/bash

ssh pi@hidi2 'fish -c "cd HIDI; go run build.go -cgo -openrgb -platforms linux-arm64"'