//go:build !ios
// +build !ios

package main

import "os"

func defaultDataPath() string {
	path, err := os.Getwd()
	if err != nil {
		return "."
	}
	return path
}
