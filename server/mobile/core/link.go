package core

import (
	"fmt"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

func parseTorrentLink(link string) (*torrent.TorrentSpec, error) {
	link = strings.TrimSpace(link)

	if strings.HasPrefix(link, "magnet:") {
		return parseMagnetLink(link)
	}

	if len(link) == 40 {
		magnetLink := "magnet:?xt=urn:btih:" + link
		return parseMagnetLink(magnetLink)
	}

	if strings.HasPrefix(link, "urn:btih:") {
		magnetLink := "magnet:?xt=" + link
		return parseMagnetLink(magnetLink)
	}

	return parseMagnetLink(link)
}

func parseMagnetLink(link string) (*torrent.TorrentSpec, error) {
	mag, err := metainfo.ParseMagnetUri(link)
	if err != nil {
		return nil, fmt.Errorf("failed to parse magnet link: %w", err)
	}

	var trackers [][]string
	if len(mag.Trackers) > 0 {
		trackers = [][]string{mag.Trackers}
	}

	return &torrent.TorrentSpec{
		Trackers:    trackers,
		DisplayName: mag.DisplayName,
		InfoHash:    mag.InfoHash,
	}, nil
}
