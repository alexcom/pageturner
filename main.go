package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

const newFileMode = os.O_APPEND | os.O_RDWR | os.O_CREATE | os.O_TRUNC

//go:generate go-bindata -pkg main -o bindata.go data/
func main() {
	pathEnv := os.Getenv("PATH")
	var err error
	if err = os.Setenv("PATH", fmt.Sprintf("%s:.", pathEnv)); err != nil {
		log.Fatal(err)
	}
	log.Println("Converting files")
	if err = parallelConvert(); err != nil {
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
	log.Println("Cleaning up")
	err = cleanup()
	if err != nil {
		log.Fatal(err)
	}
}

// TODO : detect best bitrate
const bitrateKb = 128

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
