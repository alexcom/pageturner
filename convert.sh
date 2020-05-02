#!/bin/bash

BITRATE=128k
find . -name "*.mp3" | parallel --lb -k "ffmpeg -y -i {} -map 0:a -c:a aac -b:a $BITRATE {.}.m4a"
