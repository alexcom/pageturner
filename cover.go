package main

import (
	"io/ioutil"
	"log"
	"os"
)

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
