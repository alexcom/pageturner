## PageTurner
This is a tool written in Go with FFMPEG CLI that I am using to convert audio-books from mp3 to m4b.

### Build requirements

- Go 1.22

### Run requirements

- ffmpeg
- ffprobe

### Building

    make

Or

    go build

### Installing on Linux and Mac

1. Make sure you have prerequisites installed
2. Build and execute

        make install

### Running
```shell
cd <Audiobook dir>
pageturner
```
If you are bold enough you can remove source mp3-s on success.
```shell
pageturner --remove-source
```