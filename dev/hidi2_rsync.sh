#!/usr/bin/bash

rsync -v -a --exclude '.git' --exclude 'builds' --exclude '.idea' --delete . pi@hidi2:/home/pi/HIDI/
