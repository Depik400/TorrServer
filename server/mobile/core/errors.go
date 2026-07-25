package core

type EngineError struct {
	Code    string
	Message string
}

func (e *EngineError) Error() string {
	return e.Code + ": " + e.Message
}

func NewEngineError(code, message string) *EngineError {
	return newEngineError(code, message)
}

func newEngineError(code, message string) *EngineError {
	return &EngineError{Code: code, Message: message}
}

const (
	ErrInvalidArgument       = "invalid_argument"
	ErrInvalidJSON           = "invalid_json"
	ErrEngineNotRunning      = "engine_not_running"
	ErrEngineAlreadyRunning  = "engine_already_running"
	ErrEngineStartFailed     = "engine_start_failed"
	ErrDatabaseOpenFailed    = "database_open_failed"
	ErrTorrentParseFailed    = "torrent_parse_failed"
	ErrTorrentAddFailed      = "torrent_add_failed"
	ErrTorrentNotFound       = "torrent_not_found"
	ErrTorrentMetadataPending = "torrent_metadata_pending"
	ErrTorrentMetadataTimeout = "torrent_metadata_timeout"
	ErrFileNotFound          = "file_not_found"
	ErrStreamNotFound        = "stream_not_found"
	ErrStreamCancelled       = "stream_cancelled"
	ErrHTTPStartFailed       = "http_start_failed"
	ErrInternalError         = "internal_error"
)
