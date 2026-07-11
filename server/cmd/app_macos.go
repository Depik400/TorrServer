//go:build darwin && !ios
// +build darwin,!ios

package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"apptrix.org/components/widget/webview"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"server"
	"server/log"
	"server/settings"
)

func runApp() error {
	if params.UI {
		log.TLogln("The --ui flag is deprecated and ignored in desktop mode")
	}
	if params.IP != "" && params.IP != "127.0.0.1" {
		log.TLogln("Ignoring custom IP in desktop mode:", params.IP)
	}
	if params.Ssl {
		log.TLogln("Ignoring SSL in desktop mode; embedded WebView uses local HTTP")
	}

	startBackgroundTasks()

	settings.Args.IP = "127.0.0.1"
	settings.Args.Port = params.Port
	settings.Args.Ssl = false
	settings.Args.SslPort = ""

	httpListener, err := listenDesktopHTTP(settings.Args.IP, settings.Args.Port)
	if err != nil {
		return err
	}
	defer func() {
		if httpListener != nil {
			_ = httpListener.Close()
		}
	}()

	if tcpAddr, ok := httpListener.Addr().(*net.TCPAddr); ok {
		settings.Args.Port = strconv.Itoa(tcpAddr.Port)
	}

	desktopApp := app.NewWithID("org.yourok.torrserver.macos")
	window := desktopApp.NewWindow("TorrServer")
	window.SetMaster()
	window.Resize(fyne.NewSize(1280, 800))
	window.SetContent(widget.NewLabel("Starting TorrServer..."))

	view, err := webview.New(window)
	if err != nil {
		return showDesktopError(window, err)
	}
	window.SetContent(view)

	if err := server.Start(httpListener); err != nil {
		return showDesktopError(window, err)
	}
	httpListener = nil

	targetURL := fmt.Sprintf("http://127.0.0.1:%s", settings.Args.Port)
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		server.Stop()
		return showDesktopError(window, err)
	}

	var shuttingDown atomic.Bool
	var closeOnce sync.Once
	shutdown := func() {
		closeOnce.Do(func() {
			shuttingDown.Store(true)
			view.Close()
			server.Stop()
			window.SetCloseIntercept(nil)
			window.Close()
			desktopApp.Quit()
		})
	}

	window.SetCloseIntercept(shutdown)

	go func() {
		if err := waitForServerReady(targetURL + "/api/echo"); err != nil {
			if shuttingDown.Load() {
				return
			}
			fyne.Do(func() {
				dialog.ShowError(err, window)
			})
			return
		}
		view.Load(parsedURL)
	}()

	go func() {
		waitErr := server.WaitServer()
		if shuttingDown.Load() || waitErr == "" {
			return
		}
		log.TLogln("Desktop app server error:", waitErr)
		fyne.Do(func() {
			dialog.ShowError(errors.New(waitErr), window)
		})
	}()

	window.ShowAndRun()

	if !shuttingDown.Load() {
		shutdown()
	}

	return nil
}

func listenDesktopHTTP(host, port string) (net.Listener, error) {
	if port == "" {
		port = "0"
	}
	return net.Listen("tcp", net.JoinHostPort(host, port))
}

func waitForServerReady(baseURL string) error {
	client := &http.Client{Timeout: 750 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for TorrServer at %s", baseURL)
}

func showDesktopError(window fyne.Window, err error) error {
	window.SetContent(widget.NewLabel("TorrServer failed to start."))
	go fyne.Do(func() {
		dialog.ShowError(err, window)
	})
	window.ShowAndRun()
	return err
}
