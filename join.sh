#!/bin/bash

META_ARTIST="FFmpeg Bayou Jug Band"
META_ALBUM_ARTIST="FFmpeg Bayou Jug Band"
META_TITLE="Decode my Heart (Let's Mux)"
META_ALBUM="Decode my Heart"
META_GENRE="Audiobook"

ffmpeg -f concat \
-safe 0 \
-i <(find $(pwd) -name "*.m4a" -exec echo file \'{}\' \; | sort ) \
-c copy \
-metadata artist=$META_ARTIST \
-metadata title=$META_TITLE \
-metadata genre=$META_GENRE \
-metadata album=$META_ALBUM  \
-metadata album=$META_ALBUM_ARTIST  \
out.m4a
