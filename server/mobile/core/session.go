package core

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"

	set "server/settings"
	"server/torr"
	mt "server/mimetype"
)

type StreamSession struct {
	ID         string
	Hash       string
	FileID     int
	FileName   string
	FileLength int64
	CreatedAt  time.Time

	LastRequest  atomic.Int64
	ActiveHTTP   atomic.Int32
	BytesServed  atomic.Int64

	DownloadedBytes atomic.Int64
	TotalBytes      int64

	state  SessionState
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	engine  *Engine
	torrent *torr.Torrent
}

func (s *StreamSession) Status() map[string]interface{} {
	tr := s.torrent
	st := tr.Status()

	var filePath string
	var fileLength int64
	for _, f := range st.FileStats {
		if f.Id == s.FileID {
			filePath = f.Path
			fileLength = f.Length
			break
		}
	}

	return map[string]interface{}{
		"sessionId":       s.ID,
		"state":           string(s.state),
		"downloadedBytes": st.PreloadedBytes,
		"totalBytes":      fileLength,
		"downloadSpeed":   st.DownloadSpeed,
		"activePeers":     st.ActivePeers,
		"totalPeers":      st.TotalPeers,
		"activeHTTP":      s.ActiveHTTP.Load(),
		"lastRequest":     s.LastRequest.Load(),
		"fileName":        filepath.Base(filePath),
		"hash":            s.Hash,
	}
}

func (s *StreamSession) StatusJSON() []byte {
	data, _ := json.Marshal(s.Status())
	return data
}

func (s *StreamSession) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.ActiveHTTP.Add(1)
	defer s.ActiveHTTP.Add(-1)
	s.LastRequest.Store(time.Now().Unix())

	if s.state == SessionCancelled || s.state == SessionFailed {
		http.Error(w, "Session ended", http.StatusGone)
		return
	}

	s.state = SessionStreaming

	tr := s.torrent
	if tr == nil {
		http.Error(w, "Torrent not found", http.StatusNotFound)
		return
	}

	st := tr.Status()
	var filePath string
	found := false
	for _, f := range st.FileStats {
		if f.Id == s.FileID {
			filePath = f.Path
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	files := tr.Files()
	if files == nil {
		http.Error(w, "Torrent files not available", http.StatusServiceUnavailable)
		return
	}

	var file *torrent.File
	for _, f := range files {
		if f.Path() == filePath {
			file = f
			break
		}
	}
	if file == nil {
		http.Error(w, "File not found in torrent", http.StatusNotFound)
		return
	}

	type readSeekCloser interface {
		Read(p []byte) (n int, err error)
		Seek(offset int64, whence int) (int64, error)
		Close() error
	}

	readerIface := tr.NewReader(file)
	if readerIface == nil {
		http.Error(w, "Cannot create reader", http.StatusInternalServerError)
		return
	}

	reader, ok := interface{}(readerIface).(readSeekCloser)
	if !ok {
		http.Error(w, "Invalid reader type", http.StatusInternalServerError)
		return
	}
	defer tr.CloseReader(readerIface)

	if set.BTsets.ResponsiveMode {
		if sr, ok := interface{}(readerIface).(interface{ SetResponsive() }); ok {
			sr.SetResponsive()
		}
	}

	mime, err := mt.MimeTypeByPath(file.Path())
	if err == nil && mime.IsMedia() {
		w.Header().Set("Content-Type", mime.String())
	}

	w.Header().Set("Accept-Ranges", "bytes")

	http.ServeContent(w, r, file.Path(), time.Unix(st.Timestamp, 0), reader)
}
