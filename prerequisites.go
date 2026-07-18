package main

import (
	"fmt"
	"os/exec"
)

func checkPrerequisites() error {
	if err := detect("ffmpeg"); err != nil {
		return err
	}
	if err := detect("ffprobe"); err != nil {
		return err
	}
	return nil
}

func detect(command string) error {
	if _, err := exec.LookPath(command); err != nil {
		return fmt.Errorf("%s executable not found in $PATH", command)
	}
	return nil
}
