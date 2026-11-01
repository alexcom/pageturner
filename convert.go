package main

import (
	"bytes"
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
	jobs := make([]poolJob, len(files))
	for i, filename := range files {
		filename := filename // go 1.21: loop variables are shared by closures
		dst := filepath.Join(convertDir, fmt.Sprintf(outputFmt, strings.TrimSuffix(filename, filepath.Ext(filename))))
		jobs[i] = poolJob{
			label: filename,
			done:  "converted " + filename,
			run: func() error {
				return runScriptArgs(ctx, targetDir, ffmpeg, convertArgs(filename, dst, aBitrate), nil)
			},
		}
	}
	return runPool(ctx, 1, jobs, updates)
}

// poolJob is one unit of work for runPool.
type poolJob struct {
	label string // worker status while it runs
	done  string // log line on success ("" for none)
	run   func() error
}

// runPool runs jobs on NumCPU+1 workers and reports worker status and
// progress for the given pipeline step. Errors are named after the job.
func runPool(ctx context.Context, step int, jobs []poolJob, updates chan<- tea.Msg) error {
	if len(jobs) == 0 {
		return nil
	}
	threads := min(runtime.NumCPU()+1, len(jobs))
	if updates != nil {
		updates <- msgInitWorkers{count: threads}
		updates <- msgProgress{step: step, completed: 0, total: len(jobs)}
	}

	in := make(chan poolJob)
	errCh := make(chan error, len(jobs))
	var wg sync.WaitGroup
	var mu sync.Mutex
	completed := 0
	for i := 0; i < threads; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range in {
				if updates != nil {
					updates <- msgWorkerUpdate{workerID: workerID, status: job.label}
				}
				if ctx.Err() != nil {
					errCh <- ctx.Err()
				} else if err := job.run(); err != nil {
					errCh <- fmt.Errorf("%s: %w", job.label, err)
				} else if updates != nil {
					if job.done != "" {
						updates <- msgLog{text: job.done}
					}
					mu.Lock()
					completed++
					done := completed
					mu.Unlock()
					// Send outside the lock so workers don't queue behind the UI.
					updates <- msgProgress{step: step, completed: done, total: len(jobs)}
				}
				if updates != nil {
					updates <- msgWorkerUpdate{workerID: workerID, status: "Idle"}
				}
			}
		}()
	}
	for _, job := range jobs {
		in <- job
	}
	close(in)
	wg.Wait()
	close(errCh)
	var errs []error
	for e := range errCh {
		if !errors.Is(e, context.Canceled) || len(errs) == 0 {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}

// convertArgs makes the ffmpeg arguments that convert src to an AAC .m4a at dst.
func convertArgs(src, dst string, aBitRate int) []string {
	return []string{
		confirm,
		input, src,
		mapping, mapIndex,
		audioCodec, aac,
		bitrate, fmt.Sprintf(brFmt, aBitRate),
		dst,
	}
}

func detectBitrate(ctx context.Context, dir string, updates chan<- tea.Msg) (int, error) {
	metaBytesChan, err := readMetadataFromFilesWithExtension(ctx, dir, ".mp3")
	if err != nil {
		return 0, err
	}
	return bitrateFromMeta(metaBytesChan, updates)
}

// detectBitrateOf is detectBitrate for a list of absolute paths.
func detectBitrateOf(ctx context.Context, paths []string) (int, error) {
	metaBytesChan, err := readMetadataFromFiles(ctx, "", paths)
	if err != nil {
		return 0, err
	}
	return bitrateFromMeta(metaBytesChan, nil)
}

func bitrateFromMeta(metaBytesChan <-chan bytes.Buffer, updates chan<- tea.Msg) (int, error) {
	var err error
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
