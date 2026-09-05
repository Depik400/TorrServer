package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"runtime/debug"

	"server/mobile/core"
)

func newOKResponse(data interface{}) *C.char {
	envelope := map[string]interface{}{
		"ok":   true,
		"data": data,
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		return newErrorResponse("internal_error", err.Error())
	}
	return toCString(string(b))
}

func newErrorResponse(code, message string) *C.char {
	envelope := map[string]interface{}{
		"ok": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	}
	b, _ := json.Marshal(envelope)
	return toCString(string(b))
}

func exportJSON(fn func() (interface{}, error)) *C.char {
	var result *C.char
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			msg := fmt.Sprintf("panic: %v\n%s", r, stack)
			result = newErrorResponse("internal_error", msg)
		}
	}()

	data, err := fn()
	if err != nil {
		if ce, ok := err.(*core.EngineError); ok {
			result = newErrorResponse(ce.Code, ce.Message)
		} else {
			result = newErrorResponse("internal_error", err.Error())
		}
		return result
	}
	result = newOKResponse(data)
	return result
}

//export TS_Version
func TS_Version() *C.char {
	return toCString(core.Version())
}

//export TS_Start
func TS_Start(configJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := &core.Engine{}
		err := eng.Start(fromCString(configJSON))
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"baseURL": eng.BaseURL(),
			"port":    eng.Port(),
			"token":   eng.AuthToken(),
		}, nil
	})
}

//export TS_Stop
func TS_Stop() *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		return nil, eng.Stop()
	})
}

//export TS_EngineStatus
func TS_EngineStatus() *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return map[string]interface{}{
				"state":        string(core.EngineStopped),
				"sessionCount": 0,
				"version":      core.Version(),
			}, nil
		}
		return eng.EngineStatus(), nil
	})
}

//export TS_AddTorrent
func TS_AddTorrent(requestJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}

		var req struct {
			Link  string `json:"link"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal([]byte(fromCString(requestJSON)), &req); err != nil {
			return nil, core.NewEngineError(core.ErrInvalidJSON, err.Error())
		}

		var result interface{}
		var addErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					addErr = core.NewEngineError(core.ErrTorrentAddFailed,
						fmt.Sprintf("torrent add panic: %v", r))
				}
			}()
			result, addErr = eng.AddTorrent(req.Link, req.Title)
		}()
		return result, addErr
	})
}

//export TS_AddTorrentFile
func TS_AddTorrentFile(requestJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}

		var req struct {
			DataB64  string `json:"dataB64"`
			Title    string `json:"title"`
			Poster   string `json:"poster"`
			Category string `json:"category"`
			Save     *bool  `json:"save"`
		}
		if err := json.Unmarshal([]byte(fromCString(requestJSON)), &req); err != nil {
			return nil, core.NewEngineError(core.ErrInvalidJSON, err.Error())
		}

		data, err := base64.StdEncoding.DecodeString(req.DataB64)
		if err != nil {
			return nil, core.NewEngineError(core.ErrInvalidArgument, "dataB64 is not valid base64: "+err.Error())
		}

		save := true
		if req.Save != nil {
			save = *req.Save
		}

		var result interface{}
		var addErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					addErr = core.NewEngineError(core.ErrTorrentAddFailed,
						fmt.Sprintf("torrent add panic: %v", r))
				}
			}()
			result, addErr = eng.AddTorrentFile(data, req.Title, req.Poster, req.Category, save)
		}()
		return result, addErr
	})
}

//export TS_TorrentStatus
func TS_TorrentStatus(hash *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		return eng.TorrentStatus(fromCString(hash))
	})
}

//export TS_ListTorrents
func TS_ListTorrents() *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		return eng.ListTorrents()
	})
}

//export TS_SetTorrent
func TS_SetTorrent(requestJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}

		var req struct {
			Hash     string `json:"hash"`
			Title    string `json:"title"`
			Poster   string `json:"poster"`
			Category string `json:"category"`
		}
		if err := json.Unmarshal([]byte(fromCString(requestJSON)), &req); err != nil {
			return nil, core.NewEngineError(core.ErrInvalidJSON, err.Error())
		}
		return nil, eng.SetTorrentMeta(req.Hash, req.Title, req.Poster, req.Category)
	})
}

//export TS_DropTorrent
func TS_DropTorrent(hash *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		return nil, eng.DropTorrent(fromCString(hash))
	})
}

//export TS_ExportTorrents
func TS_ExportTorrents() *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		s, err := eng.ExportTorrents()
		if err != nil {
			return nil, err
		}
		return s, nil
	})
}

//export TS_ImportTorrents
func TS_ImportTorrents(backupJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		added, skipped, failed, err := eng.ImportTorrents(fromCString(backupJSON))
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"added":   added,
			"skipped": skipped,
			"failed":  failed,
		}, nil
	})
}

//export TS_WarmupTorrents
func TS_WarmupTorrents() {
	eng := core.GetEngine()
	if eng != nil {
		eng.WarmupTorrents()
	}
}

//export TS_PrepareStream
func TS_PrepareStream(requestJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}

		var req struct {
			Hash   string `json:"hash"`
			FileID int    `json:"fileId"`
		}
		if err := json.Unmarshal([]byte(fromCString(requestJSON)), &req); err != nil {
			return nil, core.NewEngineError(core.ErrInvalidJSON, err.Error())
		}
		return eng.PrepareStream(req.Hash, req.FileID)
	})
}

//export TS_StreamStatus
func TS_StreamStatus(sessionID *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		return eng.StreamStatus(fromCString(sessionID))
	})
}

//export TS_CancelStream
func TS_CancelStream(sessionID *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		return nil, eng.CancelStream(fromCString(sessionID))
	})
}

//export TS_ChunkMap
func TS_ChunkMap(requestJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}

		var req struct {
			SessionID string `json:"sessionId"`
			PrevGen   int    `json:"prevGen,omitempty"`
		}
		if err := json.Unmarshal([]byte(fromCString(requestJSON)), &req); err != nil {
			return nil, core.NewEngineError(core.ErrInvalidJSON, err.Error())
		}
		return eng.ChunkMap(req.SessionID, req.PrevGen)
	})
}

//export TS_Settings
func TS_Settings() *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		return eng.Settings(), nil
	})
}

//export TS_SetRateLimits
func TS_SetRateLimits(requestJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}

		var req struct {
			DownloadKB int `json:"downloadKB"`
			UploadKB   int `json:"uploadKB"`
		}
		if err := json.Unmarshal([]byte(fromCString(requestJSON)), &req); err != nil {
			return nil, core.NewEngineError(core.ErrInvalidJSON, err.Error())
		}
		return nil, eng.SetRateLimits(req.DownloadKB, req.UploadKB)
	})
}

//export TS_SetUploadPolicy
func TS_SetUploadPolicy(requestJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}

		var req struct {
			Seed        bool `json:"seed"`
			AllowUpload bool `json:"allowUpload"`
		}
		if err := json.Unmarshal([]byte(fromCString(requestJSON)), &req); err != nil {
			return nil, core.NewEngineError(core.ErrInvalidJSON, err.Error())
		}
		// allowUpload is the UI-facing sense; the core takes noUpload.
		return nil, eng.SetUploadPolicy(req.Seed, !req.AllowUpload)
	})
}

//export TS_UpdateSettings
func TS_UpdateSettings(requestJSON *C.char) *C.char {
	return exportJSON(func() (interface{}, error) {
		eng := core.GetEngine()
		if eng == nil {
			return nil, core.NewEngineError(core.ErrEngineNotRunning, "engine is not running")
		}
		return nil, eng.UpdateSettings(fromCString(requestJSON))
	})
}
