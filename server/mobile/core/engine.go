package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"

	"server/log"
	sets "server/settings"
	"server/torr"
	"server/version"
)

type Engine struct {
	mu    sync.Mutex
	state EngineState
	cfg   Config

	bt *torr.BTServer

	listener   net.Listener
	httpServer *http.Server
	baseURL    string
	authToken  string
	port       int

	ctx    context.Context
	cancel context.CancelFunc

	sessions   map[string]*StreamSession
	sessionSeq int64
}

var (
	globalEngine *Engine
	engineMu     sync.Mutex
)

func GetEngine() *Engine {
	engineMu.Lock()
	defer engineMu.Unlock()
	return globalEngine
}

func (e *Engine) State() EngineState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

func (e *Engine) Port() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.port
}

func (e *Engine) BaseURL() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.baseURL
}

func (e *Engine) AuthToken() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.authToken
}

func (e *Engine) Start(configJSON string) error {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return err
	}

	engineMu.Lock()
	if globalEngine != nil {
		engineMu.Unlock()
		return newEngineError(ErrEngineAlreadyRunning, "engine is already running")
	}
	engineMu.Unlock()

	e.mu.Lock()
	if e.state != EngineStopped && e.state != EngineFailed {
		e.mu.Unlock()
		return newEngineError(ErrEngineAlreadyRunning, "engine is not in stopped state")
	}
	e.state = EngineStarting
	e.cfg = *cfg
	e.mu.Unlock()

	var startErr error
	defer func() {
		e.mu.Lock()
		if startErr != nil {
			e.state = EngineFailed
		}
		e.mu.Unlock()
	}()

	e.ctx, e.cancel = context.WithCancel(context.Background())

	sets.Path = cfg.ApplicationSupportPath

	if err := sets.InitSetsE(false, false); err != nil {
		startErr = newEngineError(ErrDatabaseOpenFailed, "failed to initialize database: "+err.Error())
		return startErr
	}

	if err := e.applyMobileSettings(cfg); err != nil {
		startErr = newEngineError(ErrEngineStartFailed, "failed to apply settings: "+err.Error())
		return startErr
	}

	e.bt = torr.NewBTS()
	if e.bt == nil {
		startErr = newEngineError(ErrEngineStartFailed, "failed to create BTS instance")
		return startErr
	}

	if err := e.bt.Connect(); err != nil {
		startErr = newEngineError(ErrEngineStartFailed, "failed to connect BTS: "+err.Error())
		return startErr
	}

	listener, err := net.Listen("tcp4", fmt.Sprintf("%s:0", cfg.ListenHost))
	if err != nil {
		listener, err = net.Listen("tcp", fmt.Sprintf("%s:0", cfg.ListenHost))
		if err != nil {
			startErr = newEngineError(ErrHTTPStartFailed, "failed to start HTTP listener: "+err.Error())
			return startErr
		}
	}

	e.listener = listener
	addr := listener.Addr().(*net.TCPAddr)
	e.port = addr.Port

	sets.Port = strconv.Itoa(e.port)
	sets.IP = cfg.ListenHost

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		startErr = newEngineError(ErrInternalError, "failed to generate auth token")
		return startErr
	}
	e.authToken = hex.EncodeToString(token)
	e.baseURL = fmt.Sprintf("http://%s:%d", cfg.ListenHost, e.port)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", e.handleHealth)
	mux.HandleFunc("/stream/", e.handleStream)
	mux.HandleFunc("/status/", e.handleStatus)

	e.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	e.sessions = make(map[string]*StreamSession)

	engineMu.Lock()
	globalEngine = e
	engineMu.Unlock()

	e.state = EngineRunning

	go func() {
		log.TLogln("Mobile HTTP server listening on", e.baseURL)
		if err := e.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.TLogln("HTTP server error:", err)
		}
	}()

	return nil
}

func (e *Engine) Stop() error {
	e.mu.Lock()
	if e.state == EngineStopped {
		e.mu.Unlock()
		return nil
	}
	if e.state != EngineRunning {
		e.mu.Unlock()
		return newEngineError(ErrEngineNotRunning, "engine is not running")
	}
	e.state = EngineStopping
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.state = EngineStopped
		e.mu.Unlock()

		engineMu.Lock()
		globalEngine = nil
		engineMu.Unlock()
	}()

	e.mu.Lock()
	for _, session := range e.sessions {
		session.Cancel()
	}
	e.mu.Unlock()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if e.httpServer != nil {
		e.httpServer.Shutdown(shutdownCtx)
		e.httpServer = nil
	}
	if e.listener != nil {
		e.listener.Close()
		e.listener = nil
	}

	if e.bt != nil {
		e.bt.Disconnect()
		e.bt = nil
	}

	torr.ShutdownGraceful()

	if e.cancel != nil {
		e.cancel()
	}

	sets.IP = ""
	sets.Port = ""
	sets.Path = ""

	return nil
}

func (e *Engine) applyMobileSettings(cfg *Config) error {
	sets.Args = &sets.ExecArgs{
		IP:        cfg.ListenHost,
		Port:      "0",
		Path:      cfg.ApplicationSupportPath,
		ProxyURL:  "",
		ProxyMode: "",
	}

	sets.TorAddr = ""

	if sets.BTsets == nil {
		sets.SetDefaultConfig()
	}

	sets.BTsets.EnableDLNA = false
	sets.BTsets.EnableRutorSearch = false
	sets.BTsets.EnableTorznabSearch = false
	sets.BTsets.DisableUPNP = true
	sets.BTsets.UseDisk = cfg.DiskCacheEnabled
	sets.BTsets.RemoveCacheOnDrop = false
	sets.BTsets.CacheSize = cfg.CacheSizeBytes
	sets.BTsets.ConnectionsLimit = cfg.ConnectionsLimit
	sets.BTsets.TorrentDisconnectTimeout = cfg.TorrentDisconnectTimeoutSeconds
	sets.BTsets.DisableUpload = cfg.DisableUpload
	sets.BTsets.TorrentsSavePath = cfg.CachePath

	if cfg.DownloadRateLimitKB > 0 {
		sets.BTsets.DownloadRateLimit = cfg.DownloadRateLimitKB
	}
	if cfg.UploadRateLimitKB > 0 {
		sets.BTsets.UploadRateLimit = cfg.UploadRateLimitKB
	}
	if cfg.DisableIPv6 {
		sets.BTsets.EnableIPv6 = false
	}

	if cfg.Debug {
		sets.BTsets.EnableDebug = true
	}

	return nil
}

func (e *Engine) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","version":"%s"}`, version.Version)
}

func (e *Engine) handleStream(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token != e.authToken {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/stream/"), "/", 2)
	if len(parts) < 2 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]

	e.mu.Lock()
	session, ok := e.sessions[sessionID]
	e.mu.Unlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	session.ServeHTTP(w, r)
}

func (e *Engine) handleStatus(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token != e.authToken {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/status/")
	e.mu.Lock()
	session, ok := e.sessions[sessionID]
	e.mu.Unlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(session.StatusJSON())
}

func (e *Engine) AddTorrent(link, title string) (map[string]interface{}, error) {
	e.mu.Lock()
	if e.state != EngineRunning {
		e.mu.Unlock()
		return nil, newEngineError(ErrEngineNotRunning, "engine is not running")
	}
	e.mu.Unlock()

	spec, err := parseTorrentLink(link)
	if err != nil {
		return nil, newEngineError(ErrTorrentParseFailed, "failed to parse torrent link: "+err.Error())
	}

	tr, err := torr.AddTorrent(spec, title, "", "", "")
	if err != nil {
		return nil, newEngineError(ErrTorrentAddFailed, "failed to add torrent: "+err.Error())
	}

	torr.SaveTorrentToDB(tr)

	hash := tr.Hash().HexString()
	return map[string]interface{}{
		"hash":  hash,
		"title": tr.Title,
		"state": "added",
	}, nil
}

func (e *Engine) TorrentStatus(hash string) (map[string]interface{}, error) {
	e.mu.Lock()
	running := e.state == EngineRunning
	e.mu.Unlock()
	if !running {
		return nil, newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	tr := torr.GetTorrent(hash)
	if tr == nil {
		return nil, newEngineError(ErrTorrentNotFound, "torrent not found: "+hash)
	}

	st := tr.Status()

	files := make([]map[string]interface{}, 0, len(st.FileStats))
	for _, f := range st.FileStats {
		files = append(files, map[string]interface{}{
			"id":     f.Id,
			"path":   f.Path,
			"length": f.Length,
		})
	}

	return map[string]interface{}{
		"hash":          st.Hash,
		"title":         st.Title,
		"state":         st.StatString,
		"torrentSize":   st.TorrentSize,
		"loadedSize":    st.LoadedSize,
		"preloadedSize": st.PreloadedBytes,
		"downloadSpeed": st.DownloadSpeed,
		"uploadSpeed":   st.UploadSpeed,
		"activePeers":   st.ActivePeers,
		"totalPeers":    st.TotalPeers,
		"files":         files,
	}, nil
}

func (e *Engine) ListTorrents() (interface{}, error) {
	e.mu.Lock()
	running := e.state == EngineRunning
	e.mu.Unlock()
	if !running {
		return nil, newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	list := torr.ListTorrent()
	var result []map[string]interface{}
	for _, tr := range list {
		st := tr.Status()
		result = append(result, map[string]interface{}{
			"hash":    st.Hash,
			"title":   st.Title,
			"state":   st.StatString,
			"size":    st.TorrentSize,
			"poster":  st.Poster,
			"added":   st.Timestamp,
		})
	}
	return result, nil
}

func (e *Engine) DropTorrent(hash string) error {
	e.mu.Lock()
	running := e.state == EngineRunning
	e.mu.Unlock()
	if !running {
		return newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	torr.RemTorrent(hash)
	return nil
}

func (e *Engine) PrepareStream(hash string, fileID int) (map[string]interface{}, error) {
	e.mu.Lock()
	if e.state != EngineRunning {
		e.mu.Unlock()
		return nil, newEngineError(ErrEngineNotRunning, "engine is not running")
	}
	e.mu.Unlock()

	tr := torr.GetTorrent(hash)
	if tr == nil {
		return nil, newEngineError(ErrTorrentNotFound, "torrent not found: "+hash)
	}

	if !tr.GotInfo() {
		return nil, newEngineError(ErrTorrentMetadataPending, "torrent metadata not yet available")
	}

	st := tr.Status()
	var filePath string
	var fileLength int64
	found := false
	for _, f := range st.FileStats {
		if f.Id == fileID {
			filePath = f.Path
			fileLength = f.Length
			found = true
			break
		}
	}
	if !found {
		return nil, newEngineError(ErrFileNotFound, fmt.Sprintf("file id %d not found in torrent", fileID))
	}

	var torrentFile *torrent.File
	files := tr.Files()
	if files != nil {
		for _, f := range files {
			if f.Path() == filePath {
				torrentFile = f
				break
			}
		}
	}

	e.sessionSeq++
	sessionID := fmt.Sprintf("s%d", e.sessionSeq)
	fileName := filepath.Base(filePath)

	ctx, cancel := context.WithCancel(e.ctx)
	session := &StreamSession{
		ID:          sessionID,
		Hash:        hash,
		FileID:      fileID,
		FileName:    fileName,
		FileLength:  fileLength,
		CreatedAt:   time.Now(),
		state:       SessionReady,
		torrentFile: torrentFile,
		engine:      e,
		torrent:     tr,
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
	}

	e.mu.Lock()
	e.sessions[sessionID] = session
	e.mu.Unlock()

	streamURL := fmt.Sprintf("%s/stream/%s/%s?token=%s", e.baseURL, sessionID, fileName, e.authToken)

	return map[string]interface{}{
		"sessionId":  sessionID,
		"url":        streamURL,
		"fileName":   fileName,
		"fileLength": fileLength,
	}, nil
}

func (e *Engine) StreamStatus(sessionID string) (map[string]interface{}, error) {
	e.mu.Lock()
	session, ok := e.sessions[sessionID]
	e.mu.Unlock()
	if !ok {
		return nil, newEngineError(ErrStreamNotFound, "session not found: "+sessionID)
	}
	return session.Status(), nil
}

func (e *Engine) CancelStream(sessionID string) error {
	e.mu.Lock()
	session, ok := e.sessions[sessionID]
	e.mu.Unlock()
	if !ok {
		return nil
	}
	session.Cancel()
	delete(e.sessions, sessionID)
	return nil
}

func (e *Engine) EngineStatus() map[string]interface{} {
	e.mu.Lock()
	defer e.mu.Unlock()

	status := map[string]interface{}{
		"state":        string(e.state),
		"version":      version.Version,
		"sessionCount": len(e.sessions),
	}

	if e.state == EngineRunning {
		status["port"] = e.port
		status["baseURL"] = e.baseURL
	}

	return status
}
