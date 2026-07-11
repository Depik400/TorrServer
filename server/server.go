package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"server/tgbot"

	"server/log"
	"server/settings"
	"server/web"
)

func Start(httpListener net.Listener) error {
	settings.InitSets(settings.Args.RDB, settings.Args.SearchWA)
	// https checks
	if settings.Args.Ssl {
		// set settings ssl enabled
		settings.Ssl = settings.Args.Ssl
		if settings.Args.SslPort == "" {
			dbSSlPort := strconv.Itoa(settings.BTsets.SslPort)
			if dbSSlPort != "0" {
				settings.Args.SslPort = dbSSlPort
			} else {
				settings.Args.SslPort = "8091"
			}
		} else { // store ssl port from params to DB
			dbSSlPort, err := strconv.Atoi(settings.Args.SslPort)
			if err == nil {
				settings.BTsets.SslPort = dbSSlPort
			}
		}
		if settings.Args.SslCert != "" && settings.Args.SslKey != "" {
			settings.BTsets.SslCert = settings.Args.SslCert
			settings.BTsets.SslKey = settings.Args.SslKey
		}
		if settings.Args.SslPort != "" {
			log.TLogln("Check web ssl port", settings.Args.SslPort)
			l, err := net.Listen("tcp", net.JoinHostPort(settings.Args.IP, settings.Args.SslPort))
			if l != nil {
				l.Close()
			}
			if err != nil {
				return fmt.Errorf("port %s already in use for HTTPS: %w", settings.Args.SslPort, err)
			}
		}
	}

	if httpListener == nil {
		if settings.Args.Port == "" {
			settings.Args.Port = "8090"
		}

		log.TLogln("Check web port", settings.Args.Port)
		var err error
		httpListener, err = net.Listen("tcp", net.JoinHostPort(settings.Args.IP, settings.Args.Port))
		if err != nil {
			return fmt.Errorf("port %s already in use for HTTP: %w", settings.Args.Port, err)
		}
	}

	if tcpAddr, ok := httpListener.Addr().(*net.TCPAddr); ok {
		settings.Args.Port = strconv.Itoa(tcpAddr.Port)
	}

	go cleanCache()
	settings.Port = settings.Args.Port
	settings.SslPort = settings.Args.SslPort
	settings.IP = settings.Args.IP

	if settings.Args.TGToken != "" {
		tgbot.Start(settings.Args.TGToken)
	}

	return web.Start(httpListener)
}

func cleanCache() {
	if !settings.BTsets.UseDisk || settings.BTsets.TorrentsSavePath == "/" || settings.BTsets.TorrentsSavePath == "" {
		return
	}

	dirs, err := os.ReadDir(settings.BTsets.TorrentsSavePath)
	if err != nil {
		return
	}

	torrs := settings.ListTorrent()

	log.TLogln("Remove unused cache in dir:", settings.BTsets.TorrentsSavePath)
	keep := map[string]bool{}
	for _, d := range dirs {
		if len(d.Name()) != 40 {
			// Not a hash
			continue
		}

		if !settings.BTsets.RemoveCacheOnDrop {
			keep[d.Name()] = true
			for _, t := range torrs {
				if d.IsDir() && d.Name() == t.InfoHash.HexString() {
					keep[d.Name()] = false
					break
				}
			}
			for hash, del := range keep {
				if del && hash == d.Name() {
					log.TLogln("Remove unused cache:", d.Name())
					removeAllFiles(filepath.Join(settings.BTsets.TorrentsSavePath, d.Name()))
				}
			}
		} else {
			if d.IsDir() {
				log.TLogln("Remove unused cache:", d.Name())
				removeAllFiles(filepath.Join(settings.BTsets.TorrentsSavePath, d.Name()))
			}
		}
	}
}

func removeAllFiles(path string) {
	files, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, f := range files {
		name := filepath.Join(path, f.Name())
		os.Remove(name)
	}
	os.Remove(path)
}

func WaitServer() string {
	err := web.Wait()
	if err != nil {
		return err.Error()
	}
	return ""
}

func Stop() {
	web.Stop()
	settings.CloseDB()
}
