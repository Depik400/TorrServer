//go:build !windows
// +build !windows

package main

import (
	"errors"
	"time"

	"github.com/pkg/browser"

	"server"
	"server/settings"
)

func runApp() error {
	startBackgroundTasks()

	if settings.Args.Port == "" {
		settings.Args.Port = "8090"
	}

	if params.UI {
		go func() {
			time.Sleep(time.Second)
			if settings.Args.Ssl {
				_ = browser.OpenURL("https://127.0.0.1:" + settings.Args.SslPort)
				return
			}
			_ = browser.OpenURL("http://127.0.0.1:" + settings.Args.Port)
		}()
	}

	if err := server.Start(nil); err != nil {
		return err
	}

	if waitErr := server.WaitServer(); waitErr != "" {
		return errors.New(waitErr)
	}

	return nil
}
