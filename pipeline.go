package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// runConversionPipeline starts the background process
func runConversionPipeline(config ConversionConfig, updates chan tea.Msg) {
	defer close(updates) // signal that the channel is closed if we exit

	updates <- msgLog{text: "Checking prerequisites..."}
	updates <- msgStepAdvance{step: 0}
	if err := checkPrerequisites(); err != nil {
		updates <- msgError{err: err}
		return
	}

	tempDir := os.TempDir()
	convertDir, err := os.MkdirTemp(tempDir, "pageturner")
	if err != nil {
		updates <- msgError{err: err}
		return
	}
	cleanupState.setConvertDir(convertDir)
	defer func() {
		err := os.RemoveAll(convertDir)
		if err != nil {
			updates <- msgLog{text: "WARN " + err.Error()}
		}
	}()

	updates <- msgLog{text: "Detecting bitrate..."}
	bitrate, err := detectBitrate(updates)
	if err != nil {
		updates <- msgError{err: err}
		return
	}

	updates <- msgStepAdvance{step: 1}
	updates <- msgLog{text: "Converting files..."}
	if err := parallelConvert(convertDir, bitrate, updates); err != nil {
		updates <- msgError{err: err}
		return
	}

	updates <- msgStepAdvance{step: 2}
	updates <- msgLog{text: "Resolving cover art..."}
	cover, tempCover, err := resolveCover(updates)
	if err != nil {
		updates <- msgError{err: err}
		return
	}

	updates <- msgStepAdvance{step: 3}
	updates <- msgLog{text: "Generating metadata file..."}
	outFilename, err := generateFFMETA(convertDir, config, updates)
	if err != nil {
		updates <- msgError{err: err}
		return
	}
	
	// Override generated outFilename if the user provided one
	if config.OutFilename != "" {
		outFilename = config.OutFilename
	}
	
	cleanupState.setMetadata(metadataFileName)
	cleanupState.setOut(outFilename)

	updates <- msgStepAdvance{step: 4}
	updates <- msgLog{text: "Merging files with metadata..."}
	if err := merge(convertDir, outFilename, cover, updates); err != nil {
		updates <- msgError{err: err}
		return
	}

	updates <- msgStepAdvance{step: 5}
	updates <- msgLog{text: "Cleaning up..."}
	err = cleanup(convertDir, tempCover, updates)
	if err != nil {
		updates <- msgError{err: err}
		return
	}
	
	cleanupState.setCover("")
	cleanupState.setOut("")
	cleanupState.setMetadata("")

	if config.RemoveSource {
		updates <- msgLog{text: "Source removal requested"}
		removeSourceFiles(config, updates)
	}
	
	updates <- msgLog{text: "Conversion successful!"}
	updates <- msgConversionDone{}
}

func removeSourceFiles(config ConversionConfig, updates chan<- tea.Msg) {
	wd := getWd()
	entries, err := os.ReadDir(wd)
	if err != nil {
		if updates != nil { updates <- msgLog{text: err.Error()} }
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mp3") {
			err = os.Remove(filepath.Join(wd, entry.Name()))
			if err != nil {
				if updates != nil {
					updates <- msgLog{text: fmt.Sprintf("error when deleting the source file \"%s\" : %v \n", entry.Name(), err)}
				}
			}
		}
	}
	
	if config.CoverPath != "" && filepath.Dir(config.CoverPath) == wd {
		err = os.Remove(config.CoverPath)
		if err != nil && updates != nil {
			updates <- msgLog{text: "WARN couldn't remove cover: " + err.Error()}
		}
	}
}

func cleanup(convertDir string, cover string, updates chan<- tea.Msg) error {
	files, err := os.ReadDir(convertDir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".m4a") {
			err = os.Remove(filepath.Join(convertDir, f.Name()))
			if err != nil {
				if updates != nil { updates <- msgLog{text: "WARN " + err.Error()} }
			}
		}
	}
	if err = os.Remove(metadataFileName); err != nil {
		if updates != nil { updates <- msgLog{text: "WARN " + err.Error()} }
	}
	if cover != "" {
		if err = os.Remove(cover); err != nil {
			if updates != nil { updates <- msgLog{text: "WARN " + err.Error()} }
		}
	}
	return nil
}
