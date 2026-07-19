package main

import (
	"os"
	"sync"
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

func (c *CleanupState) clean() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.convertDir != "" {
		_ = os.RemoveAll(c.convertDir)
	}
	if c.metadataFile != "" {
		_ = os.Remove(c.metadataFile)
	}
	if c.mergeListFile != "" {
		_ = os.Remove(c.mergeListFile)
	}
	if c.coverFile != "" {
		_ = os.Remove(c.coverFile)
	}
	if c.outFile != "" {
		_ = os.Remove(c.outFile)
	}
}
