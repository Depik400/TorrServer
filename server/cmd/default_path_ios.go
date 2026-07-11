//go:build ios
// +build ios

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
	return filepath.Join(home, "Documents", "TorrServer")
}
