package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

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
	if e.state != "" && e.state != EngineStopped && e.state != EngineFailed {
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
	mux.HandleFunc("/downloadzip", e.handleDownloadZip)

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
		ProxyURL:  cfg.ProxyURL,
		ProxyMode: cfg.ProxyMode,
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
	sets.BTsets.Seed = cfg.Seed
	sets.BTsets.TorrentsSavePath = cfg.CachePath

	// Always propagate the global rate limits (0 == unlimited). btserver.configure()
	// now always builds a live-tunable limiter, so passing 0 is fine and keeps a
	// handle for Engine.SetRateLimits.
	sets.BTsets.DownloadRateLimit = cfg.DownloadRateLimitKB
	sets.BTsets.UploadRateLimit = cfg.UploadRateLimitKB
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

func (e *Engine) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.TLogln("handleDownloadZip panic:", rec)
		}
	}()

	token := r.URL.Query().Get("token")
	if token != e.authToken {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	hashHex := r.URL.Query().Get("torrent")
	if hashHex == "" {
		http.Error(w, "Missing torrent parameter", http.StatusBadRequest)
		return
	}

	tr := torr.GetTorrent(hashHex)
	if tr == nil {
		http.Error(w, "Torrent not found", http.StatusNotFound)
		return
	}

	// Block until metadata is available, mirroring web/api/zip.go. This also
	// bumps the disconnect timeout so the torrent is not dropped mid-archive.
	if !tr.GotInfo() {
		http.Error(w, "Torrent connection timeout", http.StatusInternalServerError)
		return
	}

	files := tr.Files()
	if len(files) == 0 {
		http.Error(w, "No files in torrent", http.StatusNotFound)
		return
	}

	info := tr.Torrent.Info()

	title := tr.Title
	if title == "" {
		title = info.Name
	}
	if title == "" {
		title = hashHex
	}
	safeName := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, title)

	// RAM-cache mode: forward-only streaming of an archive much larger than the
	// cache still works, but is slow. Log a heads-up.
	if !sets.BTsets.UseDisk && sets.BTsets.CacheSize > 0 {
		var total int64
		for _, f := range files {
			total += f.Length()
		}
		if total > sets.BTsets.CacheSize*3 {
			log.TLogln("handleDownloadZip: archive size", total, "much larger than RAM cache", sets.BTsets.CacheSize, "- download will be slow")
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, safeName))

	// Keep the torrent alive for the whole copy. tr.NewReader / tr.CloseReader
	// register a cache reader and bump the expiry, but a single long-running
	// io.Copy can still outrun TorrentDisconnectTimeout, so refresh periodically.
	stopKeepAlive := make(chan struct{})
	var keepAliveOnce sync.Once
	stopKeepAliveFn := func() { keepAliveOnce.Do(func() { close(stopKeepAlive) }) }
	defer stopKeepAliveFn()
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopKeepAlive:
				return
			case <-ticker.C:
				if t := torr.GetTorrent(hashHex); t != nil {
					t.AddExpiredTime(2 * time.Minute)
				}
			}
		}
	}()

	flusher, _ := w.(http.Flusher)

	zipw := zip.NewWriter(w)
	copyFailed := false

	for _, f := range files {
		zf, err := zipw.Create(info.Name + "/" + f.DisplayPath())
		if err != nil {
			log.TLogln("handleDownloadZip: error creating zip entry:", err)
			copyFailed = true
			break
		}

		reader := tr.NewReader(f)
		if reader == nil {
			log.TLogln("handleDownloadZip: cannot create reader for", f.DisplayPath())
			copyFailed = true
			break
		}

		_, err = io.Copy(zf, reader)
		tr.CloseReader(reader)

		if err != nil {
			log.TLogln("handleDownloadZip: error copying", f.DisplayPath(), "to zip:", err)
			copyFailed = true
			break
		}

		if flusher != nil {
			flusher.Flush()
		}
	}

	if copyFailed {
		// Marker entry so the client can tell the archive is partial.
		if mw, err := zipw.Create(safeName + ".INCOMPLETE"); err == nil {
			io.WriteString(mw, "This archive is incomplete: the download was interrupted or a read error occurred.\n")
		}
	}

	if err := zipw.Close(); err != nil {
		log.TLogln("handleDownloadZip: error closing zip:", err)
	}
	stopKeepAliveFn()
	if flusher != nil {
		flusher.Flush()
	}
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

	return e.addTorrentSpec(spec, title, "", "", true)
}

// AddTorrentFile ingests a raw .torrent file (bencoded metainfo) natively via
// metainfo.Load + torrent.TorrentSpecFromMetaInfo, then reuses the same code
// path as AddTorrent. Unlike the old client-side infohash extraction this
// preserves trackers, the private flag, web seeds and InfoBytes.
func (e *Engine) AddTorrentFile(data []byte, title, poster, category string, save bool) (map[string]interface{}, error) {
	e.mu.Lock()
	if e.state != EngineRunning {
		e.mu.Unlock()
		return nil, newEngineError(ErrEngineNotRunning, "engine is not running")
	}
	e.mu.Unlock()

	if len(data) == 0 {
		return nil, newEngineError(ErrInvalidArgument, "empty torrent file")
	}

	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, newEngineError(ErrTorrentParseFailed, "failed to parse .torrent file: "+err.Error())
	}

	spec := torrent.TorrentSpecFromMetaInfo(mi)
	if spec == nil || spec.InfoHash == (metainfo.Hash{}) {
		return nil, newEngineError(ErrTorrentParseFailed, "invalid .torrent file: missing info hash")
	}
	// Guarantee InfoBytes (trackers/private/web-seeds live inside) are carried
	// through so export/import can round-trip the full metadata.
	if len(spec.InfoBytes) == 0 {
		spec.InfoBytes = mi.InfoBytes
	}

	return e.addTorrentSpec(spec, title, poster, category, save)
}

// addTorrentSpec is the shared tail of AddTorrent / AddTorrentFile: it hands a
// fully-formed *torrent.TorrentSpec to the engine and (optionally) persists it.
func (e *Engine) addTorrentSpec(spec *torrent.TorrentSpec, title, poster, category string, save bool) (map[string]interface{}, error) {
	tr, err := torr.AddTorrent(spec, title, poster, "", category)
	if err != nil {
		return nil, newEngineError(ErrTorrentAddFailed, "failed to add torrent: "+err.Error())
	}

	if save {
		saveSpec := tr.TorrentSpec
		saveTitle := tr.Title
		savePoster := tr.Poster
		saveCategory := tr.Category
		go func() {
			defer func() { _ = recover() }()
			if saveSpec == nil {
				return
			}
			t := new(sets.TorrentDB)
			t.TorrentSpec = saveSpec
			t.Title = saveTitle
			t.Poster = savePoster
			t.Category = saveCategory
			sets.AddTorrent(t)
		}()
	}

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
	result := make([]map[string]interface{}, 0)
	for _, tr := range list {
		st := tr.Status()
		result = append(result, map[string]interface{}{
			"hash":          st.Hash,
			"title":         st.Title,
			"state":         st.StatString,
			"size":          st.TorrentSize,
			"loadedSize":    st.BytesReadUsefulData,
			"downloadSpeed": st.DownloadSpeed,
			"uploadSpeed":   st.UploadSpeed,
			"poster":        st.Poster,
			"added":         st.Timestamp,
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

func (e *Engine) SetTorrentMeta(hash, title, poster, category string) error {
	e.mu.Lock()
	running := e.state == EngineRunning
	e.mu.Unlock()
	if !running {
		return newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	// An empty title makes torr.SetTorrent fall back to auto-detecting the name
	// from the torrent info, which is not what a rename request means. Reject it.
	if strings.TrimSpace(title) == "" {
		return newEngineError(ErrInvalidArgument, "title must not be empty")
	}

	if torr.SetTorrent(hash, title, poster, category, "") == nil {
		return newEngineError(ErrTorrentNotFound, "torrent not found: "+hash)
	}
	return nil
}

// SetRateLimits applies global download/upload rate limits (KB/s, 0 == unlimited)
// to the live torrent client without a reconnect, and persists them to BTsets.
func (e *Engine) SetRateLimits(downKB, upKB int) error {
	e.mu.Lock()
	running := e.state == EngineRunning
	bt := e.bt
	e.mu.Unlock()
	if !running || bt == nil {
		return newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	if downKB < 0 {
		downKB = 0
	}
	if upKB < 0 {
		upKB = 0
	}

	bt.SetRateLimits(downKB*1024, upKB*1024)

	if sets.BTsets != nil {
		sets.BTsets.DownloadRateLimit = downKB
		sets.BTsets.UploadRateLimit = upKB
		sets.SaveBTSets()
	}
	return nil
}

// SetUploadPolicy applies the global seeding policy to the live torrent client
// without a reconnect, and persists it to BTsets. seed => keep seeding after a
// download completes; noUpload => disable all upload.
func (e *Engine) SetUploadPolicy(seed, noUpload bool) error {
	e.mu.Lock()
	running := e.state == EngineRunning
	bt := e.bt
	e.mu.Unlock()
	if !running || bt == nil {
		return newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	bt.SetUploadPolicy(seed, noUpload)

	if sets.BTsets != nil {
		sets.BTsets.Seed = seed
		sets.BTsets.DisableUpload = noUpload
		sets.SaveBTSets()
	}
	return nil
}

// --- Backup: export / import -------------------------------------------------

const backupFormatVersion = 1

type backupEnvelope struct {
	Version    int             `json:"version"`
	App        string          `json:"app"`
	ExportedAt string          `json:"exportedAt"`
	Torrents   []backupTorrent `json:"torrents"`
}

type backupTorrent struct {
	InfoHash     string     `json:"infohash"`
	Trackers     [][]string `json:"trackers,omitempty"`
	Title        string     `json:"title,omitempty"`
	Poster       string     `json:"poster,omitempty"`
	Category     string     `json:"category,omitempty"`
	Data         string     `json:"data,omitempty"`
	Timestamp    int64      `json:"timestamp"`
	InfoBytesB64 *string    `json:"infoBytesB64"`
}

// ExportTorrents serialises the persisted torrent list (settings.ListTorrent)
// into the v1 backup format. InfoBytes, when present on a row (natively-ingested
// .torrent), are carried as base64 so trackers / private flag / web seeds
// round-trip; otherwise infoBytesB64 is null and only infohash + trackers are
// stored.
func (e *Engine) ExportTorrents() (string, error) {
	e.mu.Lock()
	running := e.state == EngineRunning
	e.mu.Unlock()
	if !running {
		return "", newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	list := sets.ListTorrent()
	env := backupEnvelope{
		Version:    backupFormatVersion,
		App:        "ITorrentStream",
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Torrents:   make([]backupTorrent, 0, len(list)),
	}

	for _, db := range list {
		if db == nil || db.TorrentSpec == nil {
			continue
		}
		entry := backupTorrent{
			InfoHash:  db.InfoHash.HexString(),
			Trackers:  db.Trackers,
			Title:     db.Title,
			Poster:    db.Poster,
			Category:  db.Category,
			Data:      db.Data,
			Timestamp: db.Timestamp,
		}
		if len(db.InfoBytes) > 0 {
			s := base64.StdEncoding.EncodeToString(db.InfoBytes)
			entry.InfoBytesB64 = &s
		}
		env.Torrents = append(env.Torrents, entry)
	}

	buf, err := json.MarshalIndent(&env, "", "  ")
	if err != nil {
		return "", newEngineError(ErrInternalError, "failed to marshal backup: "+err.Error())
	}
	return string(buf), nil
}

// ImportTorrents parses a v1 backup and persists each entry through the same DB
// path the add flow uses (settings.AddTorrent). Entries whose infohash is
// already present count as skipped; malformed entries as failed. Requires the
// engine to be running (settings/DB must be initialised), mirroring the
// add-torrent flow; WarmupTorrents() is called afterwards so freshly imported
// rows become live torrents.
func (e *Engine) ImportTorrents(jsonStr string) (added, skipped, failed int, err error) {
	e.mu.Lock()
	running := e.state == EngineRunning
	e.mu.Unlock()
	if !running {
		return 0, 0, 0, newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	var env backupEnvelope
	if uerr := json.Unmarshal([]byte(jsonStr), &env); uerr != nil {
		return 0, 0, 0, newEngineError(ErrInvalidJSON, uerr.Error())
	}
	if env.Version != backupFormatVersion {
		return 0, 0, 0, newEngineError(ErrInvalidArgument,
			fmt.Sprintf("unsupported backup version %d (expected %d)", env.Version, backupFormatVersion))
	}

	existing := make(map[string]struct{})
	for _, db := range sets.ListTorrent() {
		if db != nil && db.TorrentSpec != nil {
			existing[strings.ToLower(db.InfoHash.HexString())] = struct{}{}
		}
	}

	for _, entry := range env.Torrents {
		spec, berr := specFromBackupEntry(entry)
		if berr != nil {
			failed++
			log.TLogln("ImportTorrents: skipping bad entry:", berr)
			continue
		}

		hashHex := strings.ToLower(spec.InfoHash.HexString())
		if _, dup := existing[hashHex]; dup {
			skipped++
			continue
		}

		t := new(sets.TorrentDB)
		t.TorrentSpec = spec
		t.Title = entry.Title
		t.Poster = entry.Poster
		t.Category = entry.Category
		t.Data = entry.Data
		t.Timestamp = entry.Timestamp
		if t.Timestamp == 0 {
			t.Timestamp = time.Now().Unix()
		}

		ok := func() bool {
			defer func() {
				if r := recover(); r != nil {
					log.TLogln("ImportTorrents: AddTorrent panic:", r)
				}
			}()
			sets.AddTorrent(t)
			return true
		}()
		if !ok {
			failed++
			continue
		}
		existing[hashHex] = struct{}{}
		added++
	}

	if added > 0 {
		e.WarmupTorrents()
	}
	return added, skipped, failed, nil
}

// specFromBackupEntry rebuilds a *torrent.TorrentSpec from one backup entry:
// from the base64 info dict when present (full metadata preserved), otherwise
// from infohash + trackers.
func specFromBackupEntry(entry backupTorrent) (*torrent.TorrentSpec, error) {
	if entry.InfoBytesB64 != nil && *entry.InfoBytesB64 != "" {
		raw, derr := base64.StdEncoding.DecodeString(*entry.InfoBytesB64)
		if derr != nil {
			return nil, fmt.Errorf("infoBytesB64 is not valid base64: %w", derr)
		}
		mi := &metainfo.MetaInfo{
			InfoBytes:    raw,
			AnnounceList: entry.Trackers,
		}
		if _, ierr := mi.UnmarshalInfo(); ierr != nil {
			return nil, fmt.Errorf("infoBytesB64 is not a valid info dict: %w", ierr)
		}
		spec := torrent.TorrentSpecFromMetaInfo(mi)
		if spec == nil || spec.InfoHash == (metainfo.Hash{}) {
			return nil, fmt.Errorf("could not derive info hash from infoBytesB64")
		}
		if len(spec.InfoBytes) == 0 {
			spec.InfoBytes = raw
		}
		return spec, nil
	}

	ih := strings.TrimSpace(entry.InfoHash)
	if len(ih) != 40 {
		return nil, fmt.Errorf("invalid infohash %q", entry.InfoHash)
	}
	var hash metainfo.Hash
	if herr := hash.FromHexString(ih); herr != nil {
		return nil, fmt.Errorf("invalid infohash %q: %w", entry.InfoHash, herr)
	}
	return &torrent.TorrentSpec{
		InfoHash:    hash,
		Trackers:    entry.Trackers,
		DisplayName: entry.Title,
	}, nil
}

func (e *Engine) WarmupTorrents() {
	e.mu.Lock()
	running := e.state == EngineRunning
	e.mu.Unlock()
	if !running {
		return
	}

	list := torr.ListTorrentsDB()
	for hash := range list {
		torr.GetTorrent(hash.HexString())
	}
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

	// Start preloading the file immediately
	session.pinPieces()

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

		if e.bt != nil {
			filled, capacity := e.bt.RamStats()
			status["ramUsed"] = filled
			status["ramTotal"] = capacity
		}
	}

	return status
}

func (e *Engine) ChunkMap(sessionID string, prevGen int) (map[string]interface{}, error) {
	e.mu.Lock()
	session, ok := e.sessions[sessionID]
	e.mu.Unlock()
	if !ok {
		return nil, newEngineError(ErrStreamNotFound, "session not found: "+sessionID)
	}

	tr := session.torrent
	if tr == nil {
		return nil, newEngineError(ErrTorrentNotFound, "torrent not available")
	}

	rawT := tr.Torrent
	if rawT == nil || rawT.Info() == nil {
		return nil, newEngineError(ErrTorrentNotFound, "torrent info not available")
	}

	pieceLen := rawT.Info().PieceLength
	allPieces := rawT.NumPieces()
	fileFirstPiece := 0
	fileLastPiece := allPieces - 1

	if session.torrentFile != nil {
		f := session.torrentFile
		fileFirstPiece = int(f.Offset() / pieceLen)
		fileLastPiece = int((f.Offset() + f.Length() - 1) / pieceLen)
		if fileLastPiece >= allPieces {
			fileLastPiece = allPieces - 1
		}
	}

	runs := rawT.PieceStateRuns()
	ranges := compressRanges(runs)

	return map[string]interface{}{
		"generation":     0,
		"pieceLength":     pieceLen,
		"pieceCount":      allPieces,
		"fileFirstPiece":  fileFirstPiece,
		"fileLastPiece":   fileLastPiece,
		"readerPieces":    []int{},
		"ranges":          ranges,
	}, nil
}

type chunkRangeJSON struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	State string `json:"state"`
}

func compressRanges(runs []torrent.PieceStateRun) []chunkRangeJSON {
	if len(runs) == 0 {
		return nil
	}

	stateStr := func(r torrent.PieceStateRun) string {
		if r.Complete && r.Ok {
			return "disk"
		}
		if int(r.Priority) == int(torrent.PiecePriorityNow) {
			return "requested"
		}
		if int(r.Priority) > int(torrent.PiecePriorityNone) {
			return "loading"
		}
		if r.Checking {
			return "loading"
		}
		return "missing"
	}

	var result []chunkRangeJSON
	current := chunkRangeJSON{From: 0, To: 0, State: stateStr(runs[0])}
	idx := 0

	for _, run := range runs {
		s := stateStr(run)
		runEnd := idx + run.Length - 1
		if s == current.State {
			current.To = runEnd
		} else {
			result = append(result, current)
			current = chunkRangeJSON{From: idx, To: runEnd, State: s}
		}
		idx += run.Length
	}
	result = append(result, current)

	return result
}

func (e *Engine) Settings() map[string]interface{} {
	return map[string]interface{}{
		"ramCacheLimitBytes":  sets.BTsets.CacheSize,
		"connectionsLimit":    sets.BTsets.ConnectionsLimit,
		"disableUpload":       sets.BTsets.DisableUpload,
		"seed":                sets.BTsets.Seed,
		"downloadRateLimitKB": sets.BTsets.DownloadRateLimit,
		"uploadRateLimitKB":   sets.BTsets.UploadRateLimit,
	}
}

func (e *Engine) UpdateSettings(jsonStr string) error {
	var req map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		return newEngineError(ErrInvalidJSON, err.Error())
	}

	if v, ok := req["ramCacheLimitBytes"]; ok {
		if val, ok := v.(float64); ok {
			sets.BTsets.CacheSize = int64(val)
		}
	}
	if v, ok := req["connectionsLimit"]; ok {
		if val, ok := v.(float64); ok {
			sets.BTsets.ConnectionsLimit = int(val)
		}
	}
	if v, ok := req["downloadRateLimitKB"]; ok {
		if val, ok := v.(float64); ok {
			sets.BTsets.DownloadRateLimit = int(val)
		}
	}
	if v, ok := req["uploadRateLimitKB"]; ok {
		if val, ok := v.(float64); ok {
			sets.BTsets.UploadRateLimit = int(val)
		}
	}
	if v, ok := req["disableUpload"]; ok {
		if val, ok := v.(bool); ok {
			sets.BTsets.DisableUpload = val
		}
	}

	return nil
}
