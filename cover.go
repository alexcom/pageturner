package main

import (
	"context"
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

// CoverKind is the cover source chosen on the dashboard.
type CoverKind int

const (
	CoverAuto    CoverKind = iota // look for a typical cover image, then extract, then default
	CoverDefault                  // embedded default image
	CoverExtract                  // picture embedded in the first mp3
	CoverFile                     // image file chosen by the user (ConversionConfig.CoverPath)
)

// resolveCover returns the path of the image to embed. Generated images
// (extracted or default) are written to convertDir, which is removed after the run.
func resolveCover(ctx context.Context, config ConversionConfig, convertDir string, updates chan<- tea.Msg) (string, error) {
	targetDir := config.TargetDir
	switch config.CoverKind {
	case CoverFile:
		if _, err := os.Stat(config.CoverPath); err == nil {
			sendLog(updates, "Using cover "+filepath.Base(config.CoverPath))
			return config.CoverPath, nil
		}
		sendLog(updates, "WARN selected cover "+config.CoverPath+" not found, detecting automatically")
	case CoverExtract:
		if path := extractCover(ctx, targetDir, convertDir, updates); path != "" {
			return path, nil
		}
		return writeDefaultCover(convertDir)
	case CoverDefault:
		return writeDefaultCover(convertDir)
	}

	if name, err := findCover(targetDir, updates); err == nil && name != "" {
		return filepath.Join(targetDir, name), nil
	} else if err != nil {
		sendLog(updates, "failed to find cover because: "+err.Error())
	}
	if path := extractCover(ctx, targetDir, convertDir, updates); path != "" {
		return path, nil
	}
	return writeDefaultCover(convertDir)
}

func writeDefaultCover(convertDir string) (string, error) {
	if len(defaultCoverBytes) == 0 {
		return "", errors.New("embedded default cover not found")
	}
	path := filepath.Join(convertDir, defaultCover)
	if err := os.WriteFile(path, defaultCoverBytes, 0644); err != nil {
		return "", err
	}
	return path, nil
}

const extractedCoverName = "cover.jpg"

func extractCover(ctx context.Context, targetDir, convertDir string, updates chan<- tea.Msg) string {
	mp3s := listFilesByExt(targetDir, ".mp3")
	if len(mp3s) == 0 {
		sendLog(updates, "WARN no mp3 files to extract cover from")
		return ""
	}
	out := filepath.Join(convertDir, extractedCoverName)
	script := []string{ffmpeg, confirm, input, mp3s[0], noAudio, out}
	err := runScriptArgs(ctx, targetDir, script[0], script[1:], nil)
	if err != nil {
		sendLog(updates, "cover extraction failed with error: "+err.Error())
		sendLog(updates, "cover extraction is unsuccessful, will use default cover")
		return ""
	}
	return out
}

func sendLog(updates chan<- tea.Msg, text string) {
	if updates != nil {
		updates <- msgLog{text: text}
	}
}

const maxImageSize = 300 * 1024

func findCover(targetDir string, updates chan<- tea.Msg) (filename string, err error) {
	candidates, err := os.ReadDir(targetDir)
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
			if _, err := os.Stat(filepath.Join(targetDir, candidate.Name())); os.IsNotExist(err) {
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
