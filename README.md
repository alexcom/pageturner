## PageTurner
This is a tool written in Go and a set of Bash scripts around FFMPEG that I am using to convert audio-books from mp3 to m4b.

### Build requirements

- go-bindata
- Go 1.13

### Run requirements

- ffmpeg
- ffprobe

### Building

    make

Or

    go generate
    go build

### Installing on Linux and Mac

1. Make sure you have prerequisites installed
2. Build and execute

        make install
