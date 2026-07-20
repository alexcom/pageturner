package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
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

func parallelConvert(ctx context.Context, targetDir, convertDir string, aBitrate int, updates chan<- tea.Msg) error {
	files := listFilesByExt(targetDir, ".mp3")
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

	if updates != nil {
		updates <- msgInitWorkers{count: threads}
	}

	convertedCount := 0
	var mu sync.Mutex

	for i := 0; i < threads; i++ {
		workerID := i
		go func(in <-chan string) {
			for filename := range in {
				if updates != nil {
					updates <- msgWorkerUpdate{workerID: workerID, status: "Converting " + filename}
				}
				err := runScriptArgs(ctx, targetDir, ffmpeg, makeArgs(targetDir, convertDir, filename, aBitrate), nil)
				if err != nil {
					errCh <- err
				} else {
					if updates != nil {
						updates <- msgLog{text: "converted " + filename}
						mu.Lock()
						convertedCount++
						updates <- msgProgress{completed: convertedCount, total: len(files)}
						mu.Unlock()
					}
				}
				if updates != nil {
					updates <- msgWorkerUpdate{workerID: workerID, status: "Idle"}
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
	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

func makeArgs(targetDir, convertDir, filename string, aBitRate int) []string {
	targetPath := filepath.Join(convertDir, fmt.Sprintf(outputFmt, strings.TrimSuffix(filename, filepath.Ext(filename))))
	return []string{
		confirm,
		input, filename,
		mapping, mapIndex,
		audioCodec, aac,
		bitrate, fmt.Sprintf(brFmt, aBitRate),
		targetPath,
	}
}



func detectBitrate(ctx context.Context, dir string, updates chan<- tea.Msg) (int, error) {
	metaBytesChan, err := readMetadataFromFilesWithExtension(ctx, dir, ".mp3")
	if err != nil {
		return 0, err
	}
	groupped := map[int]int{}
	count := 0
	for buffer := range metaBytesChan {
		var data container
		err = json.Unmarshal(buffer.Bytes(), &data)
		if err != nil {
			return 0, err
		}
		br, err := strconv.Atoi(data.Format.BitRate)
		if err != nil {
			return 0, err
		}
		br = br / 1000 // metadata contains bits, I need kbps
		groupped[standardBitrate(br)]++
		count++
	}

	// 1 all files are equal  = use files' bitrate
	if len(groupped) == 1 {
		for key := range groupped {
			return key, nil
		}
	}
	
	sum := 0
	if updates != nil {
		updates <- msgLog{text: "Source bit rates:"}
	}
	for k, v := range groupped {
		if updates != nil {
			updates <- msgLog{text: fmt.Sprintf("%d kbps %d files", k, v)}
		}
		sum += k * v
	}
	result := standardBitrate(sum / count)
	if updates != nil {
		updates <- msgLog{text: fmt.Sprintf("Using bitrate %d kbps", result)}
	}
	return result, nil
}

// helps with non-standard VBR bitrate
func standardBitrate(br int) int {
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
