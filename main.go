package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
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
const newFileMode = os.O_APPEND | os.O_RDWR | os.O_CREATE | os.O_TRUNC

func main() {
	pathEnv := os.Getenv("PATH")
	if err := os.Setenv("PATH", fmt.Sprintf("%s:.", pathEnv)); err != nil {
		log.Fatal(err)
	}
	log.Println("Converting files")
	convert()
	log.Println("Generating metadata file")
	generateFFMETA()
	log.Println("Searching for cover")
	resolveCover()
	log.Println("Merging files with metadata")
	merge()
}

const convertScript = "page_turner_convert.sh"

const mergeScript = "page_turner_merge.sh"

// TODO : detect best bitrate
const bitrateKb = 128

func merge() {
	runScript(mergeScript, nil)
}

func convert() {
	runScript(convertScript, []string{fmt.Sprintf("BITRATE=%dk", bitrateKb)})
}

func runScript(script string, env []string) {
	command := exec.Command(script)
	for _, e := range env {
		command.Env = append(os.Environ(), e)
	}
	bb := bytes.Buffer{}
	command.Stdout = &bb
	command.Stderr = &bb
	wd, _ := os.Getwd()
	command.Dir = wd
	err := command.Run()
	if err != nil {
		err2 := writeOutputToFile(bb)
		log.Fatal(err, err2)
	}
}

func writeOutputToFile(bb bytes.Buffer) error {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	filename := filepath.Join(wd, fmt.Sprintf("fail-%s.log", time.Now().Format("2006-01-02_15:04:05")))
	file, err := os.OpenFile(filename, newFileMode, 0644)
	if err != nil {
		return err
	}
	defer closeDeferred(file)
	_, err = bb.WriteTo(file)
	if err != nil {
		return err
	}
	return nil
}

func generateFFMETA() {
	files := listFiles()
	if len(files) == 0 {
		fmt.Println("no m4a files found, check conversion results")
		return
	}

	tagLines, err := getTagLines(files[0])
	if err != nil {
		log.Fatal(err)
	}
	commonTags := toCommonTagMap(tagLines)

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
			log.Fatal(err)
		}
		chapterTags := toFullTagMap(tagLines)
		durationString := chapterTags["format.duration"]
		durationString = durationString[1 : len(durationString)-4] //remove dquotes
		durationString = strings.Replace(durationString, ".", "", 1)
		subsec, err := strconv.Atoi(durationString)
		if err != nil {
			log.Fatal(err)
		}
		start = end
		end = start + subsec
		var title string
		var ok bool
		if title, ok = chapterTags["format.title"]; !ok {
			if title, ok = chapterTags["format.filename"]; ok {
				title = title[1 : len(title)-1]
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
	file, err := os.OpenFile("FFMETA", newFileMode, 0644)
	if err != nil {
		log.Fatal("ERROR : opening FFMETA for writing", err)
	}
	defer closeDeferred(file)
	err = tt.Execute(file, data)
	if err != nil {
		log.Fatal(err)
	}
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
			result[split[0][len(prefix):]] = split[1]
		}
	}
	return result
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

func closeDeferred(file *os.File) {
	if file != nil {
		err := file.Close()
		if err != nil {
			log.Println("WARN : error closing file ", file.Name(), err)
		}
	}
}

const defaultCover = "default_cover.png"

func resolveCover() string {
	if name := findCover(); name != "" {
		return name
	}
	if name := extractCover(); name != "" {
		return name
	}
	return defaultCover
}

const coverScript = "page_turner_cover.sh"

const extractedCoverName = "cover.jpg"

func extractCover() string {
	runScript(coverScript, nil)
	return extractedCoverName
}

func findCover() string {
	for _, candidate := range []string{
		"cover.jpg",
		"Cover.jpg",
		"cover.png",
		"Cover.png",
	} {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			continue
		}
		return candidate
	}
	return ""
}
