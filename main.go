package main

import (
	"bytes"
	"errors"
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

//go:generate go-bindata -pkg main -o bindata.go data/
func main() {
	pathEnv := os.Getenv("PATH")
	var err error
	if err = os.Setenv("PATH", fmt.Sprintf("%s:.", pathEnv)); err != nil {
		log.Fatal(err)
	}
	log.Println("Converting files")
	if err = convert(); err != nil {
		log.Fatal(err)
	}
	log.Println("Generating metadata file")
	outFilename, err := generateFFMETA()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Searching for cover")
	cover := resolveCover()
	log.Println("Merging files with metadata")
	if err = merge(outFilename, cover); err != nil {
		log.Fatal(err)
	}
	log.Println("Cleaning up")
	err = cleanup()
	if err != nil {
		log.Fatal(err)
	}
}

const convertScript = "page_turner_convert.sh"

const mergeScript = "page_turner_merge.sh"

// TODO : detect best bitrate
const bitrateKb = 128

func convert() error {
	return runScript(convertScript, []string{fmt.Sprintf("BITRATE=%dk", bitrateKb)})
}

func merge(filename, cover string) error {
	return runScript(mergeScript, []string{
		fmt.Sprintf("OUT_NAME=%s", filename),
		fmt.Sprintf("COVER_NAME=%s", cover),
	})
}

func cleanup() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	files, err := ioutil.ReadDir(wd)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !f.IsDir() && (strings.HasSuffix(f.Name(), ".m4a") || f.Name() == metadataFileName) {
			err = os.Remove(f.Name())
			if err != nil {
				log.Println("WARN", err)
			}
		}
	}
	return nil
}

func runScript(script string, env []string) (err error) {
	command := exec.Command(script)
	command.Env = os.Environ()
	for _, e := range env {
		command.Env = append(command.Env, e)
	}
	bb := bytes.Buffer{}
	command.Stdout = &bb
	command.Stderr = &bb
	wd, _ := os.Getwd()
	command.Dir = wd
	err = command.Run()
	if err != nil {
		writeOutputToFile(bb)
		return err
	}
	return nil
}

func writeOutputToFile(bb bytes.Buffer) {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	filename := filepath.Join(wd, fmt.Sprintf("fail-%s.log", time.Now().Format("2006-01-02_15:04:05")))
	file, err := os.OpenFile(filename, newFileMode, 0644)
	if err != nil {
		fmt.Println(err)
	}
	defer closeDeferred(file)
	_, err = bb.WriteTo(file)
	if err != nil {
		fmt.Println(err)
	}
}

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
	data, err := Asset("data/" + defaultCover)
	if err != nil {
		log.Fatal(err)
	}
	err = ioutil.WriteFile(defaultCover, data, 0644)
	if err != nil {
		log.Fatal(err)
	}
	return defaultCover
}

const coverScript = "page_turner_cover.sh"

const extractedCoverName = "cover.jpg"

func extractCover() string {
	err := runScript(coverScript, nil)
	if err != nil {
		// assuming we will use default cover, so no fatality
		log.Println(err)
		return ""
	}
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
