#!/usr/bin/bash

rsync -v -a --exclude '.git' --exclude 'builds' --exclude '.idea' --delete . pi@hidi:/home/pi/HIDI/
