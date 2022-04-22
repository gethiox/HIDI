#!/bin/bash

echo "$(date) - compiling..."
GOOS=linux GOARCH=arm GOARM=6 go build -o ./builds/hidi cmd/*.go
if [[  $? -ne 0 ]]; then
  echo "$(date) - compilation failed"
  exit 1
fi

echo "$(date) - compilation done"
exit 0