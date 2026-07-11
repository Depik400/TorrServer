//go:build !ios && !darwin
// +build !ios,!darwin

package main

import "os"

func defaultDataPath() string {
	path, err := os.Getwd()
	if err != nil {
		return "."
	}
	return path
}

func defaultLogPath(dataPath string) string {
	return ""
}
