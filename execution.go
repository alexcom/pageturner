package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runScript(script string, env []string) (err error) {
	return runScriptArgs(script, nil, env)
}

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
		log.Fatal(err)
	}
	filename := filepath.Join(wd, fmt.Sprintf("fail-%s.log", time.Now().Format("2006-01-02_15:04:05")))
	file, err := os.OpenFile(filename, newFileMode, 0644)
	if err != nil {
		fmt.Println(err)
	}
	defer closeDeferred(file)
	_, err = bb.WriteTo(file)
	if err != nil {
		fmt.Println(err)
	}
}

func closeDeferred(file *os.File) {
	if file != nil {
		err := file.Close()
		if err != nil {
			log.Println("WARN : error closing file ", file.Name(), err)
		}
	}
}
