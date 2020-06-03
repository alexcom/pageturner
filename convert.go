package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
)

const ffmpeg = "ffmpeg"
const confirm = "-y"
const input = "-i"
const mapping = "-map"
const mapIndex = "0:a"
const codec = "-c:a"
const aac = "aac"
const bitrate = "-b:a"
const brFmt = "%dk"
const outputFmt = "%s.m4a"

func parallelConvert() error {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	files, err := ioutil.ReadDir(wd)
	if err != nil {
		log.Fatal(err)
	}

	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		if !strings.HasSuffix(file.Name(), ".mp3") {
			files = append(files[:i], files[i+1:]...)
		}
	}
	errCh := make(chan error, len(files))
	in := make(chan string)
	wg := sync.WaitGroup{}
	wg.Add(len(files))

	var threads = runtime.NumCPU() + 1 //+ runtime.NumCPU()/2
	for i := 0; i < threads; i++ {
		go func(in <-chan string) {
			for filename := range in {
				err := runScriptArgs(ffmpeg, makeArgs(filename), nil)
				if err != nil {
					errCh <- err
				} else {
					log.Println("converted", filename)
				}
				wg.Done()
			}
		}(in)
	}

	for _, file := range files {
		in <- file.Name()
	}
	close(in)
	wg.Wait()
	close(errCh)
	return <-errCh
}

func makeArgs(filename string) []string {
	return []string{
		confirm,
		input, filename,
		mapping, mapIndex,
		codec, aac,
		bitrate, fmt.Sprintf(brFmt, bitrateKb),
		fmt.Sprintf(outputFmt, filename[:len(filename)-4]),
	}
}