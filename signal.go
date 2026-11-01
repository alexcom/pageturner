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
	outFile       string
	outFiles      map[string]bool // stories being written
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

func (c *CleanupState) setOut(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outFile = path
}

// addOut and removeOut track partial outputs while several are written at once.
func (c *CleanupState) addOut(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outFiles == nil {
		c.outFiles = map[string]bool{}
	}
	c.outFiles[path] = true
}

func (c *CleanupState) removeOut(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.outFiles, path)
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
	if c.outFile != "" {
		_ = os.Remove(c.outFile)
	}
	for path := range c.outFiles {
		_ = os.Remove(path)
	}
}
