package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

const (
	ffmpeg     = "ffmpeg"
	confirm    = "-y"
	input      = "-i"
	mapping    = "-map"
	mapIndex   = "0:a"
	audioCodec = "-c:a"
	aac        = "aac"
	bitrate    = "-b:a"
	brFmt      = "%dk"
	outputFmt  = "%s.m4a"
)

func parallelConvert(convertDir string, bitrate int) error {
	files := listFilesByExt(getWd(), ".mp3")
	if len(files) == 0 {
		return errors.New("no MP3 files discovered in current directory")
	}
	errCh := make(chan error, len(files))
	in := make(chan string)
	wg := sync.WaitGroup{}
	wg.Add(len(files))

	var threads = runtime.NumCPU() + 1
	if threads > len(files) {
		threads = len(files)
	}
	for i := 0; i < threads; i++ {
		go func(in <-chan string) {
			for filename := range in {
				err := runScriptArgs(ffmpeg, makeArgs(convertDir, filename, bitrate), nil)
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
		in <- file
	}
	close(in)
	wg.Wait()
	close(errCh)
	return <-errCh
}

func makeArgs(convertDir, filename string, aBitRate int) []string {
	targetPath := filepath.Join(convertDir, fmt.Sprintf(outputFmt, filename[:len(filename)-4]))
	return []string{
		confirm,
		input, filename,
		mapping, mapIndex,
		audioCodec, aac,
		bitrate, fmt.Sprintf(brFmt, aBitRate),
		targetPath,
	}
}

type bitrateContainer struct {
	Format struct {
		BitRate string `json:"bit_rate"`
	} `json:"format"`
}

func detectBitrate() int {
	metaBytesChan, err := readMetadataFromFilesWithExtension(getWd(), ".mp3")
	if err != nil {
		log.Fatal(err)
	}
	groupped := map[int]int{}
	count := 0
	for buffer := range metaBytesChan {
		data := bitrateContainer{}
		err = json.Unmarshal(buffer.Bytes(), &data)
		if err != nil {
			log.Fatalln(err)
		}
		br, err := strconv.Atoi(data.Format.BitRate)
		if err != nil {
			log.Fatalln(err)
		}
		br = br / 1000 // metadata contains bits, I need kbps
		groupped[standardBitrate(br)]++
		count++
	}

	// 1 all files are equal  = use files' bitrate
	if len(groupped) == 1 {
		for key := range groupped { // this looks funny
			return key
		}
	}
	// 2 less than 50% of files are lower quality = majority bitrate(higher)
	// 3 all files are of various bitrate, no leaders = compute weighted average bitrate, round up
	// note : trying really naive approach here
	sum := 0
	log.Println("Source bit rates:")
	for k, v := range groupped {
		log.Println(k, "kbps ", v, " files")
		sum += k * v
	}
	// TODO: maybe using same standardBitrate method here is not the best idea.
	//  E.g. 160x5+128x5+64x1 is still closer to 128 than to 160 but quality difference may be sensible.
	result := standardBitrate(sum / count)
	log.Println("Using bitrate ", result, "kbps")
	return result
}

// helps with non-standard VBR bitrate
func standardBitrate(br int) int {
	// hell, I have no  idea what I am doing here...
	stdbr := []float64{32, 64, 96, 128, 160, 192, 256, 320}
	closest := 0
	for i, a := range stdbr {
		fbr := float64(br)
		if math.Abs(a-fbr) < math.Abs(stdbr[closest]-fbr) {
			closest = i
		}
	}
	return int(stdbr[closest])
}
