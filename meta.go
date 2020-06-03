package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"text/template"
)

const ffmetadataTemplate = `;FFMETADATA1
major_brand=mp42
minor_version=0
compatible_brands=M4A mp42isom
{{ range $k,$v := .CommonMeta }}{{$k}}={{$v}}
{{ end -}}
{{ range $track := .Chapters -}}
[CHAPTER]
TIMEBASE=1/1000
START={{ $track.Start }}
END={{ $track.End }}
title={{ $track.Title }}
{{ end }}
`

const metadataFileName = "FFMETA"

type track struct {
	Title string
	Start int
	End   int
}

type container struct {
	Format format `json:"format"`
}

type format struct {
	Filename  string `json:"filename"`
	StartTime string `json:"start_time"`
	Duration  string `json:"duration"`
	Tags      struct {
		Album  string `json:"album"`
		Genre  string `json:"genre"`
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Disc   string `json:"disc"`
		Track  string `json:"track"`
	} `json:"tags"`
}

type tagsContainer struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

func generateFFMETA() (filename string, err error) {
	files := listM4AFiles()
	if len(files) == 0 {
		return "", errors.New("no m4a files found, check conversion results and error logs")
	}
	wg := sync.WaitGroup{}
	wg.Add(len(files))
	var threads = runtime.NumCPU() + 1
	inCh := make(chan string)
	outCh := make(chan bytes.Buffer)
	for i := 0; i < threads; i++ {
		go func(input chan string, output chan bytes.Buffer) {
			for filename := range input {
				bb, err := getMetaJsonBytes(filename)
				if err != nil {
					log.Println("ERROR", err)
				}
				outCh <- bb
				wg.Done()
			}
		}(inCh, outCh)
	}
	for _, file := range files {
		inCh <- file
	}
	close(inCh)
	wg.Wait()

	var start = 0
	var end = 0
	tracks := make([]track, 0)
	var counter = 0
	tagBag := tagsContainer{}
	var fileMeta container
	for metaJsonBytes := range outCh {
		if counter == 0 {
			err = json.Unmarshal(metaJsonBytes.Bytes(), &tagBag)
			if err != nil {
				return
			}
		}
		if err = json.Unmarshal(metaJsonBytes.Bytes(), &fileMeta); err != nil {
			return
		}
		if start, end, err = parseAppendDuration(start, end, fileMeta.Format.Duration); err != nil {
			return
		}
		title := selectTitle(fileMeta.Format, counter)
		counter++
		tracks = append(tracks, track{title, start, end})
	}

	data := struct {
		Chapters   []track
		CommonMeta map[string]string
	}{
		Chapters:   tracks,
		CommonMeta: tagBag.Format.Tags,
	}
	tt := template.Must(template.New("ffmetadata").Parse(ffmetadataTemplate))
	file, err := os.OpenFile(metadataFileName, newFileMode, 0644)
	if err != nil {
		return "", err
	}
	defer closeDeferred(file)
	err = tt.Execute(file, data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s - %s.m4b", fileMeta.Format.Tags.Artist, fileMeta.Format.Tags.Album), nil
}

func selectTitle(meta format, counter int) (title string) {
	if title := meta.Tags.Title; title == "" {
		if title = meta.Filename; title != "" {
			title = cutOffExtension(title)
		} else {
			title = fmt.Sprintf("%04d", counter)
		}
	}
	return
}

func parseAppendDuration(start, end int, durationString string) (rStart int, rEnd int, err error) {
	durationString = durationString[:len(durationString)-3] // trimming trailing zeroes
	durationString = strings.Replace(durationString, ".", "", 1)
	subsec, err := strconv.Atoi(durationString)
	if err != nil {
		return 0, 0, err
	}
	return end, start + subsec, nil
}

func cutOffExtension(filename string) string {
	lastDotIndex := strings.LastIndex(filename, ".")
	if lastDotIndex == -1 {
		return filename
	}
	return filename[0:lastDotIndex]
}

func listM4AFiles() []string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	dirContent, err := ioutil.ReadDir(wd)
	if err != nil {
		log.Fatal(err)
	}
	result := []string{}
	for _, file := range dirContent {
		if file.IsDir() {
			continue
		}
		if strings.HasSuffix(file.Name(), ".m4a") {
			result = append(result, file.Name())
		}
	}
	return result
}

func getMetaJsonBytes(filename string) (c bytes.Buffer, err error) {
	const commandStart = "ffprobe -hide_banner -of json -v quiet -show_entries format"
	commandArr := strings.FieldsFunc(commandStart, func(a rune) bool {
		return a == ' '
	})
	cmd := exec.Command(commandArr[0], append(commandArr[1:], filename)...)
	bb := bytes.Buffer{}
	cmd.Stdout = &bb
	err = cmd.Run()
	return
}
