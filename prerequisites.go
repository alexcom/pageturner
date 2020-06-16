package main

import (
	"log"
	"os/exec"
)

func checkPrerequisites() {
	detect("ffmpeg")
	detect("ffprobe")
}

func detect(command string) {
	if _, err := exec.LookPath(command); err != nil {
		log.Fatal(command, "executable not found in $PATH")
	} else {
		log.Println(command, "found")
	}
}
