package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runScriptArgs(ctx context.Context, dir string, script string, args []string, env []string) (err error) {
	command := exec.CommandContext(ctx, script, args...)
	command.Env = os.Environ()
	for _, e := range env {
		command.Env = append(command.Env, e)
	}
	bb := bytes.Buffer{}
	command.Stdout = &bb
	command.Stderr = &bb
	command.Dir = dir
	err = command.Run()
	if err != nil {
		writeOutputToFile(dir, bb)
		return err
	}
	return nil
}

func writeOutputToFile(dir string, bb bytes.Buffer) {
	filename := filepath.Join(dir, fmt.Sprintf("fail-%s.log", time.Now().Format("2006-01-02_15_04_05")))
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
