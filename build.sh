#!/bin/bash

GOOS=linux GOARCH=arm GOARM=6 go build -o ./builds/hidi cmd/main.go
if [[  $? -ne 0 ]]; then
  read -n 1
fi