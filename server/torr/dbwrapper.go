package torr

import (
	"encoding/json"

	"server/log"
	"server/settings"
	"server/torr/state"
	"server/torr/utils"

	"github.com/anacrolix/torrent/metainfo"
)

type tsFiles struct {
	TorrServer struct {
		Files []*state.TorrentFileStat `json:"Files"`
	} `json:"TorrServer"`
}

func AddTorrentDB(torr *Torrent) {
	defer func() {
		if r := recover(); r != nil {
			log.TLogln("AddTorrentDB panic:", r)
		}
	}()

	t := new(settings.TorrentDB)
	t.TorrentSpec = torr.TorrentSpec
	t.Title = torr.Title
	t.Category = torr.Category
	if torr.Data == "" {
		files := new(tsFiles)
		status := torr.Status()
		if status != nil {
			files.TorrServer.Files = status.FileStats
		}
		buf, err := json.Marshal(files)
		if err == nil {
			t.Data = string(buf)
			torr.Data = t.Data
		}
	} else {
		t.Data = torr.Data
	}
	if utils.CheckImgUrl(torr.Poster) {
		t.Poster = torr.Poster
	}
	t.Size = torr.Size
	if t.Size == 0 && torr.Torrent != nil {
		t.Size = torr.Torrent.Length()
	}
	t.Timestamp = torr.Timestamp

	// Preserve the persisted per-file priority map: this rebuild is driven from
	// a *Torrent and does not carry it, so without this a rename (SetTorrent ->
	// AddTorrentDB) would wipe the user's file selection.
	if t.TorrentSpec != nil {
		for _, ex := range settings.ListTorrent() {
			if ex != nil && ex.TorrentSpec != nil && ex.InfoHash == t.InfoHash && len(ex.FilePriorities) > 0 {
				t.FilePriorities = ex.FilePriorities
				break
			}
		}
	}

	if t.TorrentSpec != nil {
		settings.AddTorrent(t)
	}
}

func GetTorrentDB(hash metainfo.Hash) *Torrent {
	list := settings.ListTorrent()
	for _, db := range list {
		if hash == db.InfoHash {
			torr := new(Torrent)
			torr.TorrentSpec = db.TorrentSpec
			torr.Title = db.Title
			torr.Poster = db.Poster
			torr.Category = db.Category
			torr.Timestamp = db.Timestamp
			torr.Size = db.Size
			torr.Data = db.Data
			torr.Stat = state.TorrentInDB
			return torr
		}
	}
	return nil
}

func RemTorrentDB(hash metainfo.Hash) {
	settings.RemTorrent(hash)
}

func ListTorrentsDB() map[metainfo.Hash]*Torrent {
	ret := make(map[metainfo.Hash]*Torrent)
	list := settings.ListTorrent()
	for _, db := range list {
		torr := new(Torrent)
		torr.TorrentSpec = db.TorrentSpec
		torr.Title = db.Title
		torr.Poster = db.Poster
		torr.Category = db.Category
		torr.Timestamp = db.Timestamp
		torr.Size = db.Size
		torr.Data = db.Data
		torr.Stat = state.TorrentInDB
		ret[torr.TorrentSpec.InfoHash] = torr
	}
	return ret
}
