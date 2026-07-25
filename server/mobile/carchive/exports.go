package main

/*
#include <stdlib.h>
*/
import "C"
import (
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
