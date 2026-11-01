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
### Conversion modes

- **Book** (default): all MP3s of a folder become one `.m4b`, one chapter per file.
- **Stories**: every MP3 becomes its own `.m4b` next to it, with its own tags and cover.
  Files are still converted in parallel.

Switch on the converter screen with the BOOK / STORIES buttons, or `ctrl+b` / `ctrl+s`
(inside tmux, `ctrl+b` is the prefix key and needs to be pressed twice).
Start in stories mode with:
```shell
pageturner --stories
```

### File manager

| Key | Action |
|-----|--------|
| `enter` / `→` | open folder |
| `←` / `backspace` | parent folder |
| `space` | select an MP3, or all MP3s of a folder |
| `a` | select all MP3s in the current folder |
| `c` | convert the selection (as stories), or the highlighted folder as a book |
| `ctrl+b` / `ctrl+s` | convert the highlighted folder as a book / as stories |
| `esc` | clear the filter or the selection |
| `/` | filter |

A selection can span several folders; each story is written next to its MP3.
