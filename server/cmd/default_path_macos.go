//go:build darwin && !ios
// +build darwin,!ios

package main

import (
	"os"
	"path/filepath"
)

func defaultDataPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "TorrServer")
	}
	return filepath.Join(home, "Library", "Application Support", "TorrServer")
}

func defaultLogPath(dataPath string) string {
	if dataPath == "" {
		dataPath = defaultDataPath()
	}
	return filepath.Join(dataPath, "torrserver.log")
}
