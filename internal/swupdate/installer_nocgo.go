//go:build !cgo
// +build !cgo

package swupdate

import (
	"errors"
	"time"
)

// ErrCGORequired is returned when CGO is required but not enabled
var ErrCGORequired = errors.New("CGO required for SWUpdate integration")

// nocgoInstaller is a stub implementation that returns errors
// when CGO is not available
type nocgoInstaller struct{}

// NewInstaller creates a new SWUpdate installer (returns error when CGO disabled)
func NewInstaller() (Installer, error) {
	return nil, ErrCGORequired
}

// NewInstallerWithPaths creates a new SWUpdate installer with custom paths
func NewInstallerWithPaths(ctrlSocketPath, progSocketPath string) (Installer, error) {
	return nil, ErrCGORequired
}

// StartUpdate always returns an error
func (c *nocgoInstaller) StartUpdate(source UpdateSource) error {
	return ErrCGORequired
}

// SendData always returns an error
func (c *nocgoInstaller) SendData(data []byte) (int, error) {
	return 0, ErrCGORequired
}

// CancelUpdate always returns an error
func (c *nocgoInstaller) CancelUpdate() error {
	return ErrCGORequired
}

// GetStatus always returns StatusIdle
func (c *nocgoInstaller) GetStatus() UpdateStatus {
	return StatusIdle
}

// IsRunning always returns false
func (c *nocgoInstaller) IsRunning() bool {
	return false
}

// GetProgress returns an empty progress struct
func (c *nocgoInstaller) GetProgress() Progress {
	return Progress{}
}

// Subscribe returns a channel that will never receive events
func (c *nocgoInstaller) Subscribe() (<-chan UpdateEvent, func()) {
	ch := make(chan UpdateEvent)
	close(ch) // Close immediately so any receive returns
	return ch, func() {}
}

// WaitForCompletion always returns failure
func (c *nocgoInstaller) WaitForCompletion(timeout time.Duration) UpdateResult {
	return UpdateResult{
		Success: false,
		Code:    -1,
		Message: ErrCGORequired.Error(),
	}
}

// Close does nothing
func (c *nocgoInstaller) Close() error {
	return nil
}
