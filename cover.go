package main

import (
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	
	tea "github.com/charmbracelet/bubbletea"
)

//go:embed data/default_cover.png
var defaultCoverBytes []byte

const (
	defaultCover = "default_cover.png"
	noAudio      = "-an"
)

func resolveCover(updates chan<- tea.Msg) (coverPath string, tempCoverPath string, err error) {
	if name, err := findCover(updates); err == nil && name != "" {
		return name, "", nil
	} else if err != nil {
		if updates != nil {
			updates <- msgLog{text: "failed to find cover because: " + err.Error()}
		}
	}
	if name := extractCover(updates); name != "" {
		cleanupState.setCover(name)
		return name, name, nil
	}
	if len(defaultCoverBytes) == 0 {
		return "", "", errors.New("embedded default cover not found")
	}
	err = os.WriteFile(defaultCover, defaultCoverBytes, 0644)
	if err != nil {
		return "", "", err
	}
	cleanupState.setCover(defaultCover)
	return defaultCover, defaultCover, nil
}

const extractedCoverName = "cover.jpg"

func extractCover(updates chan<- tea.Msg) string {
	mp3s := listFilesByExt(getWd(), ".mp3")
	if len(mp3s) == 0 {
		if updates != nil {
			updates <- msgLog{text: "WARN no mp3 files to extract cover from"}
		}
		return ""
	}
	script := []string{ffmpeg, confirm, input, mp3s[0], noAudio, extractedCoverName}
	err := runScriptArgs(script[0], script[1:], nil)
	if err != nil {
		if updates != nil {
			updates <- msgLog{text: "cover extraction failed with error: " + err.Error()}
			updates <- msgLog{text: "cover extraction is unsuccessful, will use default cover"}
		}
		return ""
	}
	return extractedCoverName
}

const maxImageSize = 300 * 1024

func findCover(updates chan<- tea.Msg) (filename string, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
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
				if updates != nil {
					updates <- msgLog{text: "holly hell! The file suddenly disappeared! Filename: " + candidate.Name()}
				}
				continue
			} else {
				return candidate.Name(), nil
			}
		}
	}
	if updates != nil {
		updates <- msgLog{text: "Looking for single image in current directory."}
	}
	if len(foundImages) == 1 {
		if f, err := foundImages[0].Info(); err == nil {
			if f.Size() <= maxImageSize {
				if updates != nil {
					updates <- msgLog{text: "Using single image found " + f.Name()}
				}
				return foundImages[0].Name(), nil
			}
		}
	}
	if updates != nil {
		updates <- msgLog{text: "cover not found"}
	}
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
