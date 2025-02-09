package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const newFileMode = os.O_APPEND | os.O_RDWR | os.O_CREATE | os.O_TRUNC

type Arguments struct {
	RemoveSource bool
}

func collectArguments() Arguments {
	result := Arguments{}
	flag.BoolVar(&result.RemoveSource, "remove-source", false, "remove MP3 files if conversion is success")
	flag.Parse()
	if flag.Parsed() {
		return result
	}
	log.Fatalln(errors.New("failed to parse CLI arguments"))
	return result
}

func main() {
	arguments := collectArguments()
	checkPrerequisites()
	tempDir := os.TempDir()
	convertDir, err := os.MkdirTemp(tempDir, "pageturner")
	if err != nil {
		log.Fatalln(err)
	}
	defer func() {
		err := os.RemoveAll(convertDir)
		if err != nil {
			log.Println("WARN", err)
		}
	}()
	log.Println("Detecting bitrate")
	bitrate := detectBitrate()
	log.Println("Converting files")
	if err = parallelConvert(convertDir, bitrate); err != nil {
		log.Fatal(err)
	}
	log.Println("Generating metadata file")
	outFilename, err := generateFFMETA(convertDir)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Searching for cover")
	cover := resolveCover()
	log.Println("Merging files with metadata")
	if err = merge(convertDir, outFilename, cover); err != nil {
		log.Fatal(err)
	}
	log.Println("Cleaning up")
	err = cleanup(convertDir)
	if err != nil {
		log.Fatal(err)
	}
	if arguments.RemoveSource {
		log.Println("source removal requested")
		removeSourceFiles()
	}
}

func removeSourceFiles() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mp3") {
			err = os.Remove(entry.Name())
			if err != nil {
				log.Printf("error when deleting the source file \"%s\" : %v \n", entry.Name(), err)
			}
		}
	}
}

func cleanup(convertDir string) error {
	files, err := os.ReadDir(convertDir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !f.IsDir() && (strings.HasSuffix(f.Name(), ".m4a") || f.Name() == metadataFileName) {
			err = os.Remove(filepath.Join(convertDir, f.Name()))
			if err != nil {
				log.Println("WARN", err)
			}
		}
	}
	return nil
}

func getWd() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return wd
}
