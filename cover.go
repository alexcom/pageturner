package main

import (
	_ "embed"
	"io/ioutil"
	"log"
	"os"
)

//go:embed data/default_cover.png
var defaultCoverBytes []byte

const defaultCover = "default_cover.png"

func resolveCover() string {
	if name := findCover(); name != "" {
		return name
	}
	if name := extractCover(); name != "" {
		return name
	}
	if len(defaultCoverBytes) == 0 {
		log.Fatal("embedded default cover not found")
	}
	err := ioutil.WriteFile(defaultCover, defaultCoverBytes, 0644)
	if err != nil {
		log.Fatal(err)
	}
	return defaultCover
}

const extractedCoverName = "cover.jpg"

func extractCover() string {
	mp3s := listFilesByExt(".mp3")
	if len(mp3s) == 0 {
		log.Println("WARN no mp3 files to extract cover from")
		return ""
	}
	script := []string{ffmpeg, confirm, input, mp3s[0], mapping, "0:v", mapping, "-0:V", extractedCoverName}
	err := runScriptArgs(script[0], script[1:], nil)
	if err != nil {
		// assuming we will use default cover, so no fatality
		log.Println("INFO cover extraction failed with error:", err)
		log.Println("INFO cover extraction is unsuccessful, will use default cover")
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
