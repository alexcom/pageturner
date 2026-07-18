package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type CleanupState struct {
	mu            sync.Mutex
	convertDir    string
	metadataFile  string
	mergeListFile string
	coverFile     string
	outFile       string
}

var cleanupState CleanupState

func (c *CleanupState) setConvertDir(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.convertDir = path
}

func (c *CleanupState) setMetadata(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metadataFile = path
}

func (c *CleanupState) setMergeList(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mergeListFile = path
}

func (c *CleanupState) setCover(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.coverFile = path
}

func (c *CleanupState) setOut(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outFile = path
}

func setupSignalHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("\nAborting... cleaning up temporary files")

		cleanupState.mu.Lock()
		defer cleanupState.mu.Unlock()

		if cleanupState.convertDir != "" {
			_ = os.RemoveAll(cleanupState.convertDir)
		}
		if cleanupState.metadataFile != "" {
			_ = os.Remove(cleanupState.metadataFile)
		}
		if cleanupState.mergeListFile != "" {
			_ = os.Remove(cleanupState.mergeListFile)
		}
		if cleanupState.coverFile != "" {
			_ = os.Remove(cleanupState.coverFile)
		}
		if cleanupState.outFile != "" {
			_ = os.Remove(cleanupState.outFile)
		}

		os.Exit(1)
	}()
}
