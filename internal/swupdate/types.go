package swupdate

import (
	"errors"
	"time"
)

// Constants for update source types
const (
	SourceUnknown          = iota
	SourceWebserver        
	SourceSuricatta        
	SourceDownloader       
	SourceLocal            
	SourceChunksDownloader 
)

// Constants for update status
type UpdateStatus int

const (
	StatusIdle      UpdateStatus = iota
	StatusStart
	StatusRun
	StatusSuccess
	StatusFailure
	StatusDownload
	StatusDone
	StatusSubprocess
	StatusProgress
)

// String returns the string representation of the status
func (s UpdateStatus) String() string {
	switch s {
	case StatusIdle:
		return "idle"
	case StatusStart:
		return "start"
	case StatusRun:
		return "run"
	case StatusSuccess:
		return "success"
	case StatusFailure:
		return "failure"
	case StatusDownload:
		return "download"
	case StatusDone:
		return "done"
	case StatusSubprocess:
		return "subprocess"
	case StatusProgress:
		return "progress"
	default:
		return "unknown"
	}
}

// Progress represents the current progress of an update
type Progress struct {
	Status         UpdateStatus `json:"status"`
	DownloadPercent int         `json:"download_percent"`
	DownloadBytes  int64       `json:"download_bytes"`
	TotalSteps     int         `json:"total_steps"`
	CurrentStep    int         `json:"current_step"`
	StepPercent    int         `json:"step_percent"`
	CurrentImage   string      `json:"current_image"`
	HandlerName    string      `json:"handler_name"`
	Message        string      `json:"message"`
	Source         int         `json:"source"`
}

// UpdateEvent represents an event during the update process
type UpdateEvent struct {
	Type     string    `json:"type"`
	Status   UpdateStatus `json:"status"`
	Progress *Progress `json:"progress,omitempty"`
	Error    error    `json:"error,omitempty"`
	Message  string   `json:"message,omitempty"`
}

// UpdateSource defines the source for an update
type UpdateSource struct {
	Type         int    // Source type constant
	FilePath     string // Path to .swu file (for local source)
	Reader       interface{} // io.Reader for streaming data (interface{} to avoid import cycle)
	FileSize     int64  // Total size of the update file
	SoftwareSet  string // Software set name (optional)
	RunningMode  string // Running mode (optional)
	DryRun       bool   // If true, only simulate the update
	BufferSize   int    // Buffer size for data transfer
}

// UpdateResult represents the result of an update operation
type UpdateResult struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Installer interface defines the methods for managing SWUpdate installations
type Installer interface {
	StartUpdate(source UpdateSource) error
	SendData(data []byte) (int, error)
	CancelUpdate() error
	GetStatus() UpdateStatus
	IsRunning() bool
	GetProgress() Progress
	Subscribe() (<-chan UpdateEvent, func())
	WaitForCompletion(timeout time.Duration) UpdateResult
	Close() error
}

// DefaultBufferSize is the default buffer size for data transfers
const DefaultBufferSize = 131072 // 128KB

// ErrUpdateInProgress is returned when trying to start an update while one is already running
var ErrUpdateInProgress = errors.New("update already in progress")

// ErrNotInitialized is returned when the SWUpdate wrapper is not initialized
var ErrNotInitialized = errors.New("SWUpdate wrapper not initialized")

// ErrCancelNotSupported is returned when trying to cancel an update (not supported by SWUpdate IPC)
var ErrCancelNotSupported = errors.New("cancel not supported by SWUpdate")

// ErrNoUpdateFile is returned when no update file or reader is provided
var ErrNoUpdateFile = errors.New("no update file or reader provided")
