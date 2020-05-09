#!/bin/bash

COVER_NAME=${COVER_NAME:-cover.jpg}
ffmpeg -y -i "$(find . -name "*.mp3" -depth 1 -print -quit)" -map 0:v -map -0:V "$COVER_NAME"
