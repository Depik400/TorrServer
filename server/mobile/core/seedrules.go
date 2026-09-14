package core

import (
	"time"

	"server/log"
	sets "server/settings"
	"server/torr"

	"github.com/anacrolix/torrent/metainfo"
)

// SeedRules controls automatic seeding-limit enforcement for finished
// downloads. Zero values mean "no limit", matching the existing rate-limit
// convention (Engine.SetRateLimits).
//
// ActionOnLimit MVP note: anacrolix exposes no direct per-torrent "stop
// uploading but keep seeding session open" API, so action 0 (pause-upload)
// is deferred (see plan t3-02 §3.1.2 / §6). Only remove actions are
// implemented; SetSeedRules rejects/normalizes anything else to
// SeedActionRemove.
type SeedRules struct {
	RatioLimit          float64 `json:"ratioLimit"`
	SeedingMinutesLimit int     `json:"seedingMinutesLimit"`
	ActionOnLimit       int     `json:"actionOnLimit"`
}

const (
	// SeedActionPause is reserved for a future release; not implemented.
	SeedActionPause = 0
	// SeedActionRemove removes the torrent but leaves any on-disk cache/files untouched.
	SeedActionRemove = 1
	// SeedActionRemoveWithData removes the torrent and deletes its on-disk cache/files.
	SeedActionRemoveWithData = 2
)

// seedRulesTickInterval is how often finished torrents are checked against
// the persisted seeding rules.
const seedRulesTickInterval = 30 * time.Second

// AutoRemovalEvent records one seeding-limit-triggered removal, for the app
// to surface as a notification and a small "recent auto-removals" log.
type AutoRemovalEvent struct {
	Hash        string `json:"hash"`
	Title       string `json:"title"`
	DeletedData bool   `json:"deletedData"`
	Timestamp   int64  `json:"timestamp"`
}

const maxAutoRemovalEvents = 50

// SetSeedRules validates and persists the seeding limit rules. They are
// applied to finished torrents on the next tick of the seeding-limits loop
// (at most seedRulesTickInterval later).
func (e *Engine) SetSeedRules(rules SeedRules) error {
	e.mu.Lock()
	running := e.state == EngineRunning
	e.mu.Unlock()
	if !running {
		return newEngineError(ErrEngineNotRunning, "engine is not running")
	}

	if rules.RatioLimit < 0 {
		rules.RatioLimit = 0
	}
	if rules.SeedingMinutesLimit < 0 {
		rules.SeedingMinutesLimit = 0
	}
	if rules.ActionOnLimit != SeedActionRemove && rules.ActionOnLimit != SeedActionRemoveWithData {
		// Also covers the deferred pause action (0) and any invalid value:
		// fall back to the data-preserving remove action.
		rules.ActionOnLimit = SeedActionRemove
	}

	if sets.BTsets != nil {
		sets.BTsets.SeedRatioLimit = rules.RatioLimit
		sets.BTsets.SeedMinutesLimit = rules.SeedingMinutesLimit
		sets.BTsets.SeedActionOnLimit = rules.ActionOnLimit
		sets.SaveBTSets()
	}
	return nil
}

// GetSeedRules returns the currently persisted seeding limit rules.
func (e *Engine) GetSeedRules() SeedRules {
	if sets.BTsets == nil {
		return SeedRules{}
	}
	return SeedRules{
		RatioLimit:          sets.BTsets.SeedRatioLimit,
		SeedingMinutesLimit: sets.BTsets.SeedMinutesLimit,
		ActionOnLimit:       sets.BTsets.SeedActionOnLimit,
	}
}

// ConsumeAutoRemovals returns and clears the pending seeding-limit
// auto-removal events. Each event is delivered to the app exactly once.
func (e *Engine) ConsumeAutoRemovals() []AutoRemovalEvent {
	e.autoRemovalsMu.Lock()
	defer e.autoRemovalsMu.Unlock()
	if len(e.autoRemovals) == 0 {
		return nil
	}
	out := e.autoRemovals
	e.autoRemovals = nil
	return out
}

func (e *Engine) recordAutoRemoval(hash, title string, deletedData bool) {
	e.autoRemovalsMu.Lock()
	defer e.autoRemovalsMu.Unlock()
	e.autoRemovals = append(e.autoRemovals, AutoRemovalEvent{
		Hash:        hash,
		Title:       title,
		DeletedData: deletedData,
		Timestamp:   time.Now().Unix(),
	})
	if len(e.autoRemovals) > maxAutoRemovalEvents {
		e.autoRemovals = e.autoRemovals[len(e.autoRemovals)-maxAutoRemovalEvents:]
	}
}

// hasActiveSession reports whether hash has a stream session that has not
// reached a terminal state (still preparing/ready/streaming/in grace). Such
// a torrent is never touched by the seeding auto-remove loop, regardless of
// ratio/time limit (hard safety requirement, plan t3-02 §5/§6).
func (e *Engine) hasActiveSession(hash string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.sessions {
		if s == nil || s.Hash != hash {
			continue
		}
		switch s.getState() {
		case SessionCancelled, SessionCompleted, SessionFailed:
			continue
		default:
			return true
		}
	}
	return false
}

// runSeedRulesLoop periodically checks every finished torrent against the
// persisted seeding rules and applies the configured action once a torrent
// exceeds its ratio or seeding-time limit. Runs until stop is closed
// (engine shutdown).
func (e *Engine) runSeedRulesLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(seedRulesTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			e.enforceSeedRulesOnce()
			e.persistUploadAccountingOnce()
		}
	}
}

// seedRuleExceeded is the pure ratio/seeding-time comparison used by
// enforceSeedRulesOnce, factored out for unit testing (a live torrent object
// needs a real BT client + DB, impractical to spin up in a unit test).
func seedRuleExceeded(rules SeedRules, ratio float64, seedingSeconds int64) bool {
	if rules.RatioLimit > 0 && ratio >= rules.RatioLimit {
		return true
	}
	if rules.SeedingMinutesLimit > 0 && seedingSeconds/60 >= int64(rules.SeedingMinutesLimit) {
		return true
	}
	return false
}

func (e *Engine) enforceSeedRulesOnce() {
	defer func() {
		if r := recover(); r != nil {
			log.TLogln("seed rules: panic:", r)
		}
	}()

	rules := e.GetSeedRules()
	if rules.RatioLimit <= 0 && rules.SeedingMinutesLimit <= 0 {
		return
	}

	for _, tr := range torr.ListTorrent() {
		if tr == nil {
			continue
		}
		st := tr.Status()

		// Only finished downloads are seeding candidates.
		if st.TorrentSize <= 0 || st.LoadedSize < st.TorrentSize {
			continue
		}

		if !seedRuleExceeded(rules, st.Ratio, st.SeedingSeconds) {
			continue
		}

		if e.hasActiveSession(st.Hash) {
			continue // never remove a torrent being actively streamed
		}

		deleteData := rules.ActionOnLimit == SeedActionRemoveWithData
		title := st.Title
		if title == "" {
			title = st.Name
		}
		log.TLogln("seed rules: auto-removing", st.Hash, "ratio:", st.Ratio, "seedingSeconds:", st.SeedingSeconds, "deleteData:", deleteData)

		if deleteData {
			torr.RemTorrent(st.Hash)
		} else {
			torr.RemTorrentKeepData(st.Hash)
		}
		e.recordAutoRemoval(st.Hash, title, deleteData)
	}
}

// persistUploadAccountingOnce refreshes the persisted uploaded-bytes
// accumulator + completion timestamp for every live torrent, so a
// restart/reconnect (which resets anacrolix's own Stats()) doesn't lose more
// than one tick's worth of upload accounting.
func (e *Engine) persistUploadAccountingOnce() {
	defer func() {
		if r := recover(); r != nil {
			log.TLogln("seed rules: persist panic:", r)
		}
	}()
	for _, tr := range torr.ListTorrent() {
		if tr == nil {
			continue
		}
		hashHex := tr.Hash().HexString()
		if hashHex == "" {
			continue
		}
		total, completedAt := tr.SnapshotUploadTotals()
		if total == 0 && completedAt == 0 {
			continue
		}
		h := metainfo.NewHashFromHex(hashHex)
		sets.UpdateUploadStats(h, total, completedAt)
	}
}
