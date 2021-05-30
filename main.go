package main

import (
	"io/ioutil"
	"log"
	"os"
	"strings"
)

const newFileMode = os.O_APPEND | os.O_RDWR | os.O_CREATE | os.O_TRUNC

func main() {
	checkPrerequisites()
	log.Println("Detecting bitrate")
	bitrate := detectBitrate()
	log.Println("Converting files")
	var err error
	if err = parallelConvert(bitrate); err != nil {
		log.Fatal(err)
	}
	log.Println("Generating metadata file")
	outFilename, err := generateFFMETA()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Searching for cover")
	cover := resolveCover()
	log.Println("Merging files with metadata")
	if err = merge(outFilename, cover); err != nil {
		log.Fatal(err)
	}
	//log.Println("Cleaning up")
	//err = cleanup()
	//if err != nil {
	//	log.Fatal(err)
	//}
}

func cleanup() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	files, err := ioutil.ReadDir(wd)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !f.IsDir() && (strings.HasSuffix(f.Name(), ".m4a") || f.Name() == metadataFileName) {
			err = os.Remove(f.Name())
			if err != nil {
				log.Println("WARN", err)
			}
		}
	}
	return nil
}
