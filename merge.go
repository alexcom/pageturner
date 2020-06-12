package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

const format_ = "-f"
const concat = "concat"
const threadQueueSize = "-thread_queue_size"
const safe = "-safe"
const mapMeta = "-map_metadata"
const codec = "-c"

func merge(filename, cover string) (err error) {
	listFileName, err := generateMergeFileList()
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
		input, "FFMETA",
		mapping, "0",
		mapping, "1",
		mapMeta, "2",
		codec, "copy",
		"-disposition:v:0", "attached_pic",
		filename,
	}
	return runScriptArgs(script[0], script[1:], nil)
}

const fileListFileName = "filelist.txt"

func generateMergeFileList() (filename string, err error) {
	files := listFilesByExt(".m4a")
	bb := bytes.Buffer{}
	for _, file := range files {
		if file, err = filepath.Abs(file); err != nil {
			return
		}
		if _, err = fmt.Fprintf(&bb, "file '%s'\n", file); err != nil {
			return
		}
	}
	if err = ioutil.WriteFile(fileListFileName, bb.Bytes(), 0644); err != nil {
		return
	}
	return fileListFileName, nil
}
