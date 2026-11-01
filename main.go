package main

import (
	"context"
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

// collectArguments parses flags and an optional mode (go, gogogo, fm).
// The mode may come before or after the flags.
func collectArguments() (Arguments, string) {
	result := Arguments{}
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.BoolVar(&result.RemoveSource, "remove-source", false, "remove MP3 files if conversion is success")
	args := os.Args[1:]
	var mode string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode, args = args[0], args[1:]
	}
	_ = fs.Parse(args)
	if mode == "" {
		mode = fs.Arg(0)
	}
	return result, mode
}

func main() {
	defer cleanupState.clean()

	arguments, mode := collectArguments()

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := tea.NewProgram(newUI(ctx, initialState, wd, arguments), tea.WithAltScreen(), tea.WithMouseCellMotion())
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
