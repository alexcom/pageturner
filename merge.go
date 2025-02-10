package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	format_         = "-f"
	concat          = "concat"
	threadQueueSize = "-thread_queue_size"
	safe            = "-safe"
	mapMeta         = "-map_metadata"
	codec           = "-c"
	codecCopy       = "copy"
	dispositionV0   = "-disposition:v:0"
	attachedPic     = "attached_pic"
)
const fileListFileName = "filelist.txt"

func merge(convertDir, filename, cover string) (err error) {
	listFileName, err := generateMergeFileList(convertDir)
	defer func() {
		err := os.Remove(listFileName)
		if err != nil {
			log.Println("WARN", listFileName, "was not deleted")
		}
	}()
	if err != nil {
		return
	}
	script := []string{
		ffmpeg,
		confirm,
		format_, concat,
		threadQueueSize, "40960",
		safe, "0",
		input, listFileName,
		input, cover,
		input, metadataFileName,
		mapping, "0",
		mapping, "1",
		mapMeta, "2",
		codec, codecCopy,
		dispositionV0, attachedPic,
		filename,
	}
	return runScriptArgs(script[0], script[1:], nil)
}

func generateMergeFileList(convertDir string) (filename string, err error) {
	files := listFilesByExt(convertDir, ".m4a")
	bb := bytes.Buffer{}
	for _, file := range files {
		file = filepath.Join(convertDir, file)
		if _, err = fmt.Fprintf(&bb, "file '%s'\n", escapeQuote(file)); err != nil {
			return
		}
	}
	if err = os.WriteFile(fileListFileName, bb.Bytes(), 0644); err != nil {
		return
	}
	return fileListFileName, nil
}

func escapeQuote(fn string) string {
	return strings.ReplaceAll(fn, "'", "'\\''")
}
