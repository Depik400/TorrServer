package core

type EngineState string

const (
	EngineStopped  EngineState = "stopped"
	EngineStarting EngineState = "starting"
	EngineRunning  EngineState = "running"
	EngineStopping EngineState = "stopping"
	EngineFailed   EngineState = "failed"
)

type SessionState string

const (
	SessionPreparing SessionState = "preparing"
	SessionReady     SessionState = "ready"
	SessionStreaming SessionState = "streaming"
	SessionGrace     SessionState = "grace"
	SessionCompleted SessionState = "completed"
	SessionCancelled SessionState = "cancelled"
	SessionFailed    SessionState = "failed"
)
