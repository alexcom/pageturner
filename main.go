package main

import (
	"flag"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const newFileMode = os.O_APPEND | os.O_RDWR | os.O_CREATE | os.O_TRUNC

type Arguments struct {
	RemoveSource bool
}

func collectArguments() Arguments {
	result := Arguments{}
	flag.BoolVar(&result.RemoveSource, "remove-source", false, "remove MP3 files if conversion is success")
	flag.Parse()
	return result
}

func main() {
	defer cleanupState.clean()

	args := os.Args[1:]
	var mode string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode = args[0]
	}

	wd := getWd()
	var initialState state

	if mode == "go" {
		initialState = stateDashboard
	} else if mode == "gogogo" {
		initialState = stateProgress
	} else if mode == "fm" {
		initialState = stateFileManager
	} else {
		files := listFilesByExt(wd, ".mp3")
		if len(files) > 0 {
			initialState = stateDashboard
		} else {
			initialState = stateFileManager
		}
	}

	p := tea.NewProgram(newUI(initialState, wd), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Alas, there's been an error: %v", err)
	}
}

func getWd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
