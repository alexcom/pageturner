package main

import (
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed data/default_cover.png
var defaultCoverBytes []byte

const defaultCover = "default_cover.png"

func resolveCover() string {
	if name, err := findCover(); err == nil && name != "" {
		return name
	} else if err != nil {
		log.Println("failed to find cover because: ", err)
	}
	if name := extractCover(); name != "" {
		return name
	}
	if len(defaultCoverBytes) == 0 {
		log.Fatal("embedded default cover not found")
	}
	err := os.WriteFile(defaultCover, defaultCoverBytes, 0644)
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

const maxImageSize = 300 * 1024

func findCover() (filename string, err error) {
	dir, err := os.Getwd()
	candidates, err := os.ReadDir(dir)
	foundImages := make([]os.DirEntry, 0)
	for _, candidate := range candidates {
		if candidate.IsDir() {
			continue
		}
		candidateNameLowerCase := strings.ToLower(candidate.Name())
		if isSupportedImageFormatFile(candidateNameLowerCase) {
			foundImages = append(foundImages, candidate)
			justName := strings.TrimSuffix(candidateNameLowerCase, filepath.Ext(candidateNameLowerCase))
			if !matchesTypicalCoverName(justName) {
				continue
			}
			if _, err := os.Stat(candidate.Name()); os.IsNotExist(err) {
				log.Println("holly hell! The file suddenly disappeared! Filename:", candidate.Name())
				continue
			} else {
				return candidate.Name(), nil
			}
		}
	}
	// Last resort, find *single* image in current dir with proper size
	log.Println("Looking for single image in current directory.")
	if len(foundImages) == 1 {
		if f, err := foundImages[0].Info(); err == nil {
			if f.Size() <= maxImageSize {
				log.Println("Using single image found", f.Name())
				return foundImages[0].Name(), nil
			}
		}
	}
	log.Println("INFO cover not found")
	return "", nil
}

func matchesTypicalCoverName(name string) bool {
	return slices.Index([]string{"cover", "folder", "image"}, name) != -1
}

func isSupportedImageFormatFile(filenameLowerCase string) bool {
	for _, suffix := range []string{".jpg", ".jpeg", ".png"} {
		if strings.HasSuffix(filenameLowerCase, suffix) {
			return true
		}
	}
	return false
}
