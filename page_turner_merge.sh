#!/bin/bash

OUT_NAME=${OUT_NAME:-out.m4b}
COVER_NAME=${COVER_NAME:-cover.jpg}

ffmpeg -y -f concat \
  -thread_queue_size 40960 \
  -safe 0 \
  -i <(find "$(pwd)" -depth 1 -name "*.m4a" -exec echo file \'{}\' \; | sort) \
  -i "$COVER_NAME" \
  -i FFMETA \
  -map 0 \
  -map 1 \
  -map_metadata 2 \
  -c copy \
  -disposition:v:0 attached_pic \
  "$OUT_NAME"
