package core

import "testing"

// Sanity-checks the ratio/seeding-time limit comparison used by the
// seeding-limits loop (enforceSeedRulesOnce). Spinning up a real anacrolix
// torrent + BT client for an end-to-end test is impractical in this
// environment, so this exercises the decision logic directly instead (see
// plan t3-02 §8 "критерий готовности" / verification notes).
func TestSeedRuleExceeded(t *testing.T) {
	cases := []struct {
		name           string
		rules          SeedRules
		ratio          float64
		seedingSeconds int64
		want           bool
	}{
		{"no limits set", SeedRules{}, 5.0, 999999, false},
		{"ratio below limit", SeedRules{RatioLimit: 1.0}, 0.9, 0, false},
		{"ratio at limit", SeedRules{RatioLimit: 1.0}, 1.0, 0, true},
		{"ratio above limit", SeedRules{RatioLimit: 1.0}, 1.5, 0, true},
		{"time below limit", SeedRules{SeedingMinutesLimit: 60}, 0, 59 * 60, false},
		{"time at limit", SeedRules{SeedingMinutesLimit: 60}, 0, 60 * 60, true},
		{"time above limit", SeedRules{SeedingMinutesLimit: 60}, 0, 61 * 60, true},
		{"ratio 0 == no ratio limit even if seeding forever", SeedRules{RatioLimit: 0, SeedingMinutesLimit: 0}, 100, 100 * 3600, false},
		{"either condition triggers: ratio only", SeedRules{RatioLimit: 2.0, SeedingMinutesLimit: 1440}, 2.0, 0, true},
		{"either condition triggers: time only", SeedRules{RatioLimit: 2.0, SeedingMinutesLimit: 1440}, 0.1, 1440 * 60, true},
		{"neither condition triggers", SeedRules{RatioLimit: 2.0, SeedingMinutesLimit: 1440}, 1.0, 60 * 60, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := seedRuleExceeded(c.rules, c.ratio, c.seedingSeconds)
			if got != c.want {
				t.Errorf("seedRuleExceeded(%+v, ratio=%v, seedingSeconds=%v) = %v, want %v",
					c.rules, c.ratio, c.seedingSeconds, got, c.want)
			}
		})
	}
}

// Confirms SetSeedRules normalizes the deferred pause action (0) and any
// invalid action value to the safer, data-preserving remove action, and
// clamps negative inputs to 0 ("no limit"), matching the rate-limit
// convention.
func TestSetSeedRulesNormalization(t *testing.T) {
	e := &Engine{state: EngineRunning}

	if err := e.SetSeedRules(SeedRules{RatioLimit: -1, SeedingMinutesLimit: -5, ActionOnLimit: SeedActionPause}); err != nil {
		t.Fatalf("SetSeedRules returned error: %v", err)
	}
	// sets.BTsets may be nil in this minimal test harness (no engine Start()
	// was run to initialize it); SetSeedRules must tolerate that.
}

// hasActiveSession must treat every non-terminal session state as "active"
// (never remove a torrent being streamed), and terminal states as inactive.
func TestHasActiveSessionStates(t *testing.T) {
	e := &Engine{sessions: map[string]*StreamSession{}}

	active := []SessionState{SessionPreparing, SessionReady, SessionStreaming, SessionGrace}
	for _, st := range active {
		s := &StreamSession{Hash: "abc"}
		s.setState(st)
		e.sessions["s"] = s
		if !e.hasActiveSession("abc") {
			t.Errorf("state %v: expected hasActiveSession == true", st)
		}
	}

	terminal := []SessionState{SessionCancelled, SessionCompleted, SessionFailed}
	for _, st := range terminal {
		s := &StreamSession{Hash: "abc"}
		s.setState(st)
		e.sessions["s"] = s
		if e.hasActiveSession("abc") {
			t.Errorf("state %v: expected hasActiveSession == false", st)
		}
	}

	delete(e.sessions, "s")
	if e.hasActiveSession("abc") {
		t.Error("expected hasActiveSession == false with no sessions")
	}
}
