package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runScriptArgs(script string, args []string, env []string) (err error) {
	command := exec.Command(script, args...)
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
		return
	}
	filename := filepath.Join(wd, fmt.Sprintf("fail-%s.log", time.Now().Format("2006-01-02_15_04_05")))
	file, err := os.OpenFile(filename, newFileMode, 0644)
	if err != nil {
		return
	}
	defer closeDeferred(file)
	_, err = bb.WriteTo(file)
	if err != nil {
		return
	}
}

func closeDeferred(file *os.File) {
	if file != nil {
		_ = file.Close()
	}
}
