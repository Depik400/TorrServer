package settings

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

type TorrentDB struct {
	*torrent.TorrentSpec

	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Data     string `json:"data,omitempty"`

	Timestamp int64 `json:"timestamp,omitempty"`
	Size      int64 `json:"size,omitempty"`

	// FilePriorities is the persisted per-file download priority map for the
	// torrent: fileId (1-based, as reported in TorrentStatus) -> priority,
	// where priority is 0 = skip, 1 = normal, 4 = high. An absent/empty field
	// means "all normal" and is migration-safe (older DB rows simply lack it).
	// The mobile engine re-applies this map on warmup so a restart keeps the
	// user's file selection instead of restarting a full download.
	FilePriorities map[int]int `json:"file_priorities,omitempty"`
}

type File struct {
	Name string `json:"name,omitempty"`
	Id   int    `json:"id,omitempty"`
	Size int64  `json:"size,omitempty"`
}

var mu sync.Mutex

func AddTorrent(torr *TorrentDB) {
	list := ListTorrent()
	mu.Lock()
	find := -1
	for i, db := range list {
		if db.InfoHash.HexString() == torr.InfoHash.HexString() {
			find = i
			break
		}
	}
	if find != -1 {
		list[find] = torr
	} else {
		list = append(list, torr)
	}
	for _, db := range list {
		buf, err := json.Marshal(db)
		if err == nil {
			tdb.Set("Torrents", db.InfoHash.HexString(), buf)
		}
	}
	mu.Unlock()
}

func ListTorrent() []*TorrentDB {
	// Use read lock to prevent migration during read
	dbMigrationLock.RLock()
	defer dbMigrationLock.RUnlock()

	mu.Lock()
	defer mu.Unlock()

	var list []*TorrentDB
	keys := tdb.List("Torrents")
	for _, key := range keys {
		buf := tdb.Get("Torrents", key)
		if len(buf) > 0 {
			var torr *TorrentDB
			err := json.Unmarshal(buf, &torr)
			if err == nil {
				list = append(list, torr)
			}
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Timestamp > list[j].Timestamp
	})
	return list
}

func RemTorrent(hash metainfo.Hash) {
	mu.Lock()
	tdb.Rem("Torrents", hash.HexString())
	mu.Unlock()
}
