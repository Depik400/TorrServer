package core

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"

	set "server/settings"
	"server/torr"
	mt "server/mimetype"
)

const graceTimeout = 60 * time.Second

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

	mu         sync.Mutex
	state      SessionState
	closeOnce  sync.Once

	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}

	graceCtx    context.Context
	cancelGrace context.CancelFunc

	torrentFile *torrent.File
	engine      *Engine
	torrent     *torr.Torrent
}

func (s *StreamSession) getState() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *StreamSession) setState(state SessionState) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
}

func (s *StreamSession) Cancel() {
	s.mu.Lock()
	if s.state == SessionCancelled || s.state == SessionCompleted || s.state == SessionFailed {
		s.mu.Unlock()
		return
	}
	s.state = SessionCancelled
	s.mu.Unlock()

	s.cancelGraceTimer()
	s.unpinPieces()
	s.cancel()
}

func (s *StreamSession) startGrace() {
	s.mu.Lock()
	if s.state == SessionCancelled || s.state == SessionFailed || s.state == SessionCompleted {
		s.mu.Unlock()
		return
	}
	s.state = SessionGrace
	s.graceCtx, s.cancelGrace = context.WithCancel(context.Background())
	s.mu.Unlock()

	timer := time.NewTimer(graceTimeout)
	defer timer.Stop()

	select {
	case <-timer.C:
		s.mu.Lock()
		if s.state == SessionGrace {
			s.state = SessionCompleted
		}
		s.mu.Unlock()
		s.unpinPieces()
		s.closeDone()
	case <-s.graceCtx.Done():
	case <-s.ctx.Done():
	}
}

func (s *StreamSession) cancelGraceTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelGrace != nil {
		s.cancelGrace()
		s.graceCtx = nil
		s.cancelGrace = nil
	}
}

func (s *StreamSession) closeDone() {
	s.closeOnce.Do(func() {
		close(s.done)
	})
}

func (s *StreamSession) pinPieces() {
	if s.torrentFile != nil {
		s.torrentFile.SetPriority(torrent.PiecePriorityNow)
	}
}

func (s *StreamSession) unpinPieces() {
	if s.torrentFile != nil {
		s.torrentFile.SetPriority(torrent.PiecePriorityNone)
	}
}

func (s *StreamSession) fileBytesCompleted() int64 {
	if s.torrentFile == nil {
		return 0
	}
	states := s.torrentFile.State()
	var completed int64
	for _, ps := range states {
		if ps.Complete {
			completed += ps.Bytes
		}
	}
	return completed
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

	downloaded := s.fileBytesCompleted()
	s.DownloadedBytes.Store(downloaded)

	return map[string]interface{}{
		"sessionId":       s.ID,
		"state":           string(s.getState()),
		"downloadedBytes": downloaded,
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
	s.cancelGraceTimer()

	s.ActiveHTTP.Add(1)
	s.LastRequest.Store(time.Now().Unix())

	s.setState(SessionStreaming)
	s.pinPieces()

	defer func() {
		s.ActiveHTTP.Add(-1)
		if s.ActiveHTTP.Load() == 0 && s.getState() == SessionStreaming {
			s.startGrace()
		}
	}()

	state := s.getState()
	if state == SessionCancelled || state == SessionFailed || state == SessionCompleted {
		http.Error(w, "Session ended", http.StatusGone)
		return
	}

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
