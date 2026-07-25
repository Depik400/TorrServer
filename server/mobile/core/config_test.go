package core

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParseConfigValid(t *testing.T) {
	cfg := Config{
		ApplicationSupportPath:          t.TempDir(),
		CachePath:                       t.TempDir(),
		ListenHost:                      "127.0.0.1",
		ListenPort:                      0,
		CacheSizeBytes:                  256 * 1024 * 1024,
		DiskCacheEnabled:                true,
		DownloadRateLimitKB:             0,
		UploadRateLimitKB:               0,
		ConnectionsLimit:                25,
		TorrentDisconnectTimeoutSeconds: 120,
		DisableUpload:                   true,
		DisableUPnP:                     true,
		DisableIPv6:                     false,
		Debug:                           false,
	}

	jsonBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseConfig(string(jsonBytes))
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if parsed.ApplicationSupportPath != cfg.ApplicationSupportPath {
		t.Errorf("ApplicationSupportPath: got %q, want %q", parsed.ApplicationSupportPath, cfg.ApplicationSupportPath)
	}
	if parsed.CachePath != cfg.CachePath {
		t.Errorf("CachePath: got %q, want %q", parsed.CachePath, cfg.CachePath)
	}
	if parsed.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost: got %q, want 127.0.0.1", parsed.ListenHost)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	jsonData := `{
		"applicationSupportPath": "` + t.TempDir() + `",
		"cachePath": "` + t.TempDir() + `"
	}`

	parsed, err := ParseConfig(jsonData)
	if err != nil {
		t.Fatalf("ParseConfig with minimal JSON failed: %v", err)
	}

	if parsed.ListenHost != "127.0.0.1" {
		t.Errorf("default ListenHost: got %q, want 127.0.0.1", parsed.ListenHost)
	}
	if parsed.ConnectionsLimit != 25 {
		t.Errorf("default ConnectionsLimit: got %d, want 25", parsed.ConnectionsLimit)
	}
	if parsed.TorrentDisconnectTimeoutSeconds != 120 {
		t.Errorf("default TorrentDisconnectTimeoutSeconds: got %d, want 120", parsed.TorrentDisconnectTimeoutSeconds)
	}
}

func TestParseConfigInvalidJSON(t *testing.T) {
	_, err := ParseConfig("{bad json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	ee, ok := err.(*EngineError)
	if !ok {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if ee.Code != ErrInvalidJSON {
		t.Errorf("error code: got %q, want %q", ee.Code, ErrInvalidJSON)
	}
}

func TestParseConfigEmptyPath(t *testing.T) {
	jsonData := `{"applicationSupportPath":"", "cachePath":"/tmp"}`
	_, err := ParseConfig(jsonData)
	if err == nil {
		t.Fatal("expected error for empty applicationSupportPath")
	}
	ee, ok := err.(*EngineError)
	if !ok {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if ee.Code != ErrInvalidArgument {
		t.Errorf("error code: got %q, want %q", ee.Code, ErrInvalidArgument)
	}
}

func TestParseConfigRelativePath(t *testing.T) {
	jsonData := `{"applicationSupportPath":"relative/path", "cachePath":"/tmp"}`
	_, err := ParseConfig(jsonData)
	if err == nil {
		t.Fatal("expected error for relative applicationSupportPath")
	}
	ee, ok := err.(*EngineError)
	if !ok {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if ee.Code != ErrInvalidArgument {
		t.Errorf("error code: got %q, want %q", ee.Code, ErrInvalidArgument)
	}
}

func TestParseConfigInvalidHost(t *testing.T) {
	jsonData := `{
		"applicationSupportPath": "` + t.TempDir() + `",
		"cachePath": "` + t.TempDir() + `",
		"listenHost": "0.0.0.0"
	}`
	_, err := ParseConfig(jsonData)
	if err == nil {
		t.Fatal("expected error for invalid listenHost")
	}
	ee, ok := err.(*EngineError)
	if !ok {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if ee.Code != ErrInvalidArgument {
		t.Errorf("error code: got %q, want %q", ee.Code, ErrInvalidArgument)
	}
}

func TestParseConfigIPv6Loopback(t *testing.T) {
	cfg := Config{
		ApplicationSupportPath: t.TempDir(),
		CachePath:              t.TempDir(),
		ListenHost:             "::1",
		ListenPort:             0,
		ConnectionsLimit:       25,
	}
	jsonBytes, _ := json.Marshal(cfg)
	parsed, err := ParseConfig(string(jsonBytes))
	if err != nil {
		t.Fatalf("ParseConfig with ::1 host failed: %v", err)
	}
	if parsed.ListenHost != "::1" {
		t.Errorf("ListenHost: got %q, want ::1", parsed.ListenHost)
	}
}

func TestParseConfigCacheSizeClamped(t *testing.T) {
	jsonData := `{
		"applicationSupportPath": "` + t.TempDir() + `",
		"cachePath": "` + t.TempDir() + `",
		"cacheSizeBytes": 1024
	}`
	parsed, err := ParseConfig(jsonData)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	minCache := int64(32 * 1024 * 1024)
	if parsed.CacheSizeBytes != minCache {
		t.Errorf("CacheSizeBytes: got %d, want %d (clamped to minimum)", parsed.CacheSizeBytes, minCache)
	}
}

func TestParseConfigCacheSizeMaxClamped(t *testing.T) {
	jsonData := `{
		"applicationSupportPath": "` + t.TempDir() + `",
		"cachePath": "` + t.TempDir() + `",
		"cacheSizeBytes": 10737418240
	}`
	parsed, err := ParseConfig(jsonData)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	maxCache := int64(1 * 1024 * 1024 * 1024)
	if parsed.CacheSizeBytes != maxCache {
		t.Errorf("CacheSizeBytes: got %d, want %d (clamped to maximum)", parsed.CacheSizeBytes, maxCache)
	}
}

func TestParseConfigInvalidPort(t *testing.T) {
	jsonData := `{
		"applicationSupportPath": "` + t.TempDir() + `",
		"cachePath": "` + t.TempDir() + `",
		"listenPort": 99999
	}`
	_, err := ParseConfig(jsonData)
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestParseConfigValidPortRange(t *testing.T) {
	for _, port := range []int{0, 80, 443, 8080, 65535} {
		cfg := Config{
			ApplicationSupportPath:          t.TempDir(),
			CachePath:                       t.TempDir(),
			ListenHost:                      "127.0.0.1",
			ListenPort:                      port,
			CacheSizeBytes:                  256 * 1024 * 1024,
			ConnectionsLimit:                25,
			TorrentDisconnectTimeoutSeconds: 120,
		}
		jsonBytes, _ := json.Marshal(cfg)
		_, err := ParseConfig(string(jsonBytes))
		if err != nil {
			t.Errorf("ParseConfig with port %d failed: %v", port, err)
		}
	}
}

func TestParseConfigCreatesDirectories(t *testing.T) {
	appSupport := t.TempDir() + "/nested/app_support"
	cache := t.TempDir() + "/nested/cache"

	jsonData := `{
		"applicationSupportPath": "` + appSupport + `",
		"cachePath": "` + cache + `"
	}`

	_, err := ParseConfig(jsonData)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if err := dirExists(appSupport); err != nil {
		t.Errorf("applicationSupportPath was not created: %v", err)
	}
	if err := dirExists(cache); err != nil {
		t.Errorf("cachePath was not created: %v", err)
	}
}

func TestEngineErrorImplementsError(t *testing.T) {
	var err error = &EngineError{Code: "test", Message: "test message"}
	if err.Error() != "test: test message" {
		t.Errorf("Error(): got %q, want %q", err.Error(), "test: test message")
	}
}

func TestNewEngineError(t *testing.T) {
	ee := NewEngineError(ErrInternalError, "something went wrong")
	if ee.Code != ErrInternalError {
		t.Errorf("Code: got %q, want %q", ee.Code, ErrInternalError)
	}
	if ee.Message != "something went wrong" {
		t.Errorf("Message: got %q, want %q", ee.Message, "something went wrong")
	}
}

func TestErrorCodeConstants(t *testing.T) {
	codes := []string{
		ErrInvalidArgument,
		ErrInvalidJSON,
		ErrEngineNotRunning,
		ErrEngineAlreadyRunning,
		ErrEngineStartFailed,
		ErrDatabaseOpenFailed,
		ErrTorrentParseFailed,
		ErrTorrentAddFailed,
		ErrTorrentNotFound,
		ErrTorrentMetadataPending,
		ErrTorrentMetadataTimeout,
		ErrFileNotFound,
		ErrStreamNotFound,
		ErrStreamCancelled,
		ErrHTTPStartFailed,
		ErrInternalError,
	}

	seen := make(map[string]bool)
	for _, code := range codes {
		if strings.TrimSpace(code) == "" {
			t.Errorf("empty error code constant found")
		}
		if seen[code] {
			t.Errorf("duplicate error code: %q", code)
		}
		seen[code] = true
	}

	if len(codes) != 16 {
		t.Errorf("expected 16 error codes, got %d", len(codes))
	}
}

func dirExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrNotExist
	}
	return nil
}
