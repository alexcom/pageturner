#!/bin/bash

BITRATE=${BITRATE:-128k}
ffmpeg -y \
  -i 00.mp3 -map 0:a -c:a aac -b:a $BITRATE 00.m4a \
  </dev/null
