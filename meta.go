package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

func generateFFMETA() (filename string, err error) {
	files := listFiles()
	if len(files) == 0 {
		return "", errors.New("no m4a files found, check conversion results")
	}

	tagLines, err := getTagLines(files[0])
	if err != nil {
		return "", err
	}
	commonTags := toCommonTagMap(tagLines)
	var artist = commonTags["artist"]
	var album = commonTags["album"]
	type track struct {
		Title string
		Start int
		End   int
	}

	tracks := make([]track, 0)
	var start = 0
	var end = 0
	for counter, file := range files {
		tagLines, err := getTagLines(file)
		if err != nil {
			return "", err
		}
		chapterTags := toFullTagMap(tagLines)
		durationString := chapterTags["format.duration"]
		durationString = durationString[1 : len(durationString)-4] //remove dquotes
		durationString = strings.Replace(durationString, ".", "", 1)
		subsec, err := strconv.Atoi(durationString)
		if err != nil {
			return "", err
		}
		start = end
		end = start + subsec
		var title string
		var ok bool
		if title, ok = chapterTags["format.title"]; !ok {
			if title, ok = chapterTags["format.filename"]; ok {
				title = cutOffExtension(dequote(title))
			} else {
				title = fmt.Sprintf("%04d", counter)
			}
		}
		tracks = append(tracks, track{
			Title: title,
			Start: start,
			End:   end,
		})
	}

	data := struct {
		Chapters   []track
		CommonMeta map[string]string
	}{
		Chapters:   tracks,
		CommonMeta: commonTags,
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
	return fmt.Sprintf("%s - %s.m4b", artist, album), nil
}

func cutOffExtension(filename string) string {
	lastDotIndex := strings.LastIndex(filename, ".")
	if lastDotIndex == -1 {
		return filename
	}
	return filename[0:lastDotIndex]
}

func listFiles() []string {
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

func toCommonTagMap(tagLines []string) map[string]string {
	result := map[string]string{}
	const prefix = "format.tags."
	for _, line := range tagLines {
		if strings.HasPrefix(line, prefix) {
			split := strings.Split(line, "=")
			if len(split) < 2 {
				continue
			}
			result[split[0][len(prefix):]] = dequote(split[1])
		}
	}
	return result
}

func dequote(s string) string {
	const dquote = '"'
	l := len(s)
	if l >= 2 {
		if s[0] == dquote && s[l-1] == dquote {
			return s[1 : l-1]
		}
	}
	return s
}

func toFullTagMap(tagLines []string) map[string]string {
	result := map[string]string{}
	for _, line := range tagLines {
		split := strings.Split(line, "=")
		if len(split) < 2 {
			continue
		}
		result[split[0]] = split[1]
	}
	return result
}

func getTagLines(filename string) ([]string, error) {
	// add file name as last argument
	const command = "ffprobe -hide_banner -of flat -v quiet -show_entries format:tags=album,artist,album_artist,title,comment"
	commandArr := strings.FieldsFunc(command, func(a rune) bool {
		return a == ' '
	})
	cmd := exec.Command(commandArr[0], append(commandArr[1:], filename)...)
	bb := bytes.Buffer{}
	cmd.Stdout = &bb
	err := cmd.Run()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(bb.String(), "\n")
	return lines, nil
}
