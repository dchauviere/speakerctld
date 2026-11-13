package swupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// simpleInstaller implements the Installer interface using a simple system call
type simpleInstaller struct {
	mu          sync.RWMutex
	running     int32 // atomic for thread safety
	status      UpdateStatus
	progress    Progress
	subscribers []chan UpdateEvent
	closeChan   chan struct{}
	closeDone   chan struct{}
	cmd         *exec.Cmd
	ctx         context.Context
	ctxCancel   context.CancelFunc
	dataFile    *os.File
	bytesSent   int64
	bufferSize  int
	currentStep int
	totalSteps  int
}

// NewInstaller creates a new SWUpdate installer using simple system calls
func NewInstaller() (Installer, error) {
	return NewInstallerWithPaths("", "")
}

// NewInstallerWithPaths creates a new SWUpdate installer (paths are ignored in this implementation)
func NewInstallerWithPaths(ctrlSocketPath, progSocketPath string) (Installer, error) {
	// Check if swupdate command is available
	if _, err := exec.LookPath("swupdate"); err != nil {
		return nil, fmt.Errorf("swupdate command not found in PATH: %w", err)
	}

	client := &simpleInstaller{
		running:    0,
		status:     StatusIdle,
		closeChan:  make(chan struct{}),
		closeDone:  make(chan struct{}),
		bufferSize: DefaultBufferSize,
	}

	return client, nil
}

// StartUpdate starts a new update with the given source
func (c *simpleInstaller) StartUpdate(source UpdateSource) error {
	if atomic.CompareAndSwapInt32(&c.running, 0, 1) == false {
		return ErrUpdateInProgress
	}
	defer func() {
		if r := recover(); r != nil {
			atomic.StoreInt32(&c.running, 0)
			panic(r)
		}
	}()

	// Validate source - we need a file path for the simple implementation
	if source.Reader == nil && source.FilePath == "" {
		atomic.StoreInt32(&c.running, 0)
		return ErrNoUpdateFile
	}

	c.mu.Lock()
	c.status = StatusStart
	c.progress = Progress{}
	c.currentStep = 0
	c.totalSteps = 0
	c.mu.Unlock()
	c.notifySubscribers(UpdateEvent{
		Type:   "started",
		Status: StatusStart,
	})

	// Start the update in a goroutine
	go c.startUpdateInternal(source)

	return nil
}

// startUpdateInternal does the actual update work using swupdate command
func (c *simpleInstaller) startUpdateInternal(source UpdateSource) {
	defer func() {
		atomic.StoreInt32(&c.running, 0)
		c.mu.Lock()
		c.status = StatusDone
		c.mu.Unlock()
		c.notifySubscribers(UpdateEvent{
			Type:   "completed",
			Status: StatusDone,
		})
	}()

	var swuFilePath string
	var cleanupFile bool

	// If we have a file path, use it directly
	if source.FilePath != "" {
		swuFilePath = source.FilePath
		cleanupFile = false
	} else if source.Reader != nil {
		// If we have a reader, save it to a temp file
		if reader, ok := source.Reader.(io.Reader); ok {
			tempFile, err := os.CreateTemp("", "swupdate-*.swu")
			if err != nil {
				c.mu.Lock()
				c.status = StatusFailure
				c.mu.Unlock()
				c.notifySubscribers(UpdateEvent{
					Type:    "failed",
					Status:  StatusFailure,
					Error:   fmt.Errorf("failed to create temp file: %w", err),
					Message: fmt.Sprintf("Failed to create temp file: %v", err),
				})
				return
			}

			// Copy data from reader to temp file
			buf := make([]byte, c.bufferSize)
			for {
				n, err := reader.Read(buf)
				if err != nil && !errors.Is(err, io.EOF) {
					c.mu.Lock()
					c.status = StatusFailure
					c.mu.Unlock()
					c.notifySubscribers(UpdateEvent{
						Type:    "failed",
						Status:  StatusFailure,
						Error:   fmt.Errorf("failed to read data: %w", err),
						Message: fmt.Sprintf("Read error: %v", err),
					})
					tempFile.Close()
					os.Remove(tempFile.Name())
					return
				}

				if n > 0 {
					if _, err := tempFile.Write(buf[:n]); err != nil {
						c.mu.Lock()
						c.status = StatusFailure
						c.mu.Unlock()
						c.notifySubscribers(UpdateEvent{
							Type:    "failed",
							Status:  StatusFailure,
							Error:   fmt.Errorf("failed to write temp file: %w", err),
							Message: fmt.Sprintf("Write error: %v", err),
						})
						tempFile.Close()
						os.Remove(tempFile.Name())
						return
					}
					c.bytesSent += int64(n)
				}

				if err != nil && errors.Is(err, io.EOF) {
					break
				}
			}

			// Close and keep the temp file
			if err := tempFile.Close(); err != nil {
				c.mu.Lock()
				c.status = StatusFailure
				c.mu.Unlock()
				c.notifySubscribers(UpdateEvent{
					Type:    "failed",
					Status:  StatusFailure,
					Error:   fmt.Errorf("failed to close temp file: %w", err),
					Message: fmt.Sprintf("Close error: %v", err),
				})
				os.Remove(tempFile.Name())
				return
			}

			swuFilePath = tempFile.Name()
			cleanupFile = true
			c.dataFile = tempFile
		} else {
			atomic.StoreInt32(&c.running, 0)
			c.mu.Lock()
			c.status = StatusFailure
			c.mu.Unlock()
			c.notifySubscribers(UpdateEvent{
				Type:    "failed",
				Status:  StatusFailure,
				Error:   fmt.Errorf("reader must implement io.Reader"),
				Message: "Reader must implement io.Reader",
			})
			return
		}
	}

	// Defer cleanup of temp file
	if cleanupFile {
		defer func() {
			if c.dataFile != nil {
				c.dataFile.Close()
				os.Remove(c.dataFile.Name())
				c.dataFile = nil
			}
		}()
	}

	// Update status
	c.mu.Lock()
	c.status = StatusRun
	c.mu.Unlock()
	c.notifySubscribers(UpdateEvent{
		Type:    "status",
		Status:  StatusRun,
		Message: fmt.Sprintf("Starting swupdate with file: %s", swuFilePath),
	})

	// Build the swupdate command
	args := []string{"-i", swuFilePath}

	// Add optional parameters
	if source.DryRun {
		args = append(args, "--dry-run")
	}
	if source.SoftwareSet != "" {
		args = append(args, "-s", source.SoftwareSet)
	}
	if source.RunningMode != "" {
		args = append(args, "-r", source.RunningMode)
	}

	cmd := exec.Command("swupdate", args...)
	c.cmd = cmd

	// Create context for timeout
	c.ctx, c.ctxCancel = context.WithCancel(context.Background())
	defer c.ctxCancel()

	// Start the command
	if err := cmd.Start(); err != nil {
		c.mu.Lock()
		c.status = StatusFailure
		c.mu.Unlock()
		c.notifySubscribers(UpdateEvent{
			Type:    "failed",
			Status:  StatusFailure,
			Error:   fmt.Errorf("failed to start swupdate: %w", err),
			Message: fmt.Sprintf("Failed to start swupdate: %v", err),
		})
		return
	}

	// Notify that update is running
	c.mu.Lock()
	c.status = StatusRun
	c.mu.Unlock()
	c.notifySubscribers(UpdateEvent{
		Type:   "progress",
		Status: StatusRun,
		Progress: &Progress{
			Status:       StatusRun,
			CurrentImage: swuFilePath,
			Message:      "Update in progress",
		},
	})

	// Wait for the command to complete
	// Use a goroutine to wait for completion and monitor
	go c.monitorCommand(cmd, swuFilePath)
}

// monitorCommand monitors the swupdate command and updates status
func (c *simpleInstaller) monitorCommand(cmd *exec.Cmd, swuFilePath string) {
	// Wait for command completion
	err := cmd.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		// Command failed
		if exitErr, ok := err.(*exec.ExitError); ok {
			status := exitErr.Sys().(syscall.WaitStatus)
			c.status = StatusFailure
			c.notifySubscribers(UpdateEvent{
				Type:    "failed",
				Status:  StatusFailure,
				Error:   fmt.Errorf("swupdate failed with exit code %d", status.ExitStatus()),
				Message: fmt.Sprintf("SWUpdate failed with exit code %d", status.ExitStatus()),
			})
		} else {
			c.status = StatusFailure
			c.notifySubscribers(UpdateEvent{
				Type:    "failed",
				Status:  StatusFailure,
				Error:   fmt.Errorf("swupdate failed: %w", err),
				Message: fmt.Sprintf("SWUpdate failed: %v", err),
			})
		}
	} else {
		// Command succeeded
		c.status = StatusSuccess
		c.notifySubscribers(UpdateEvent{
			Type:    "completed",
			Status:  StatusSuccess,
			Message: "Update completed successfully",
		})
	}
}

// SendData is not used in simple implementation (data is sent via file)
// It returns an error as this implementation uses file-based updates
func (c *simpleInstaller) SendData(data []byte) (int, error) {
	// In this simple implementation, we don't support streaming
	// All data must be provided via file path in StartUpdate
	return 0, fmt.Errorf("streaming not supported in simple implementation")
}

// CancelUpdate attempts to cancel the current update
func (c *simpleInstaller) CancelUpdate() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.IsRunning() {
		return nil
	}

	// Try to cancel the context
	if c.ctxCancel != nil {
		c.ctxCancel()
	}

	// Try to kill the command
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil {
			// Process might have already finished
			return nil
		}
	}

	c.status = StatusIdle
	c.notifySubscribers(UpdateEvent{
		Type:    "status",
		Status:  StatusIdle,
		Message: "Update cancelled",
	})

	return nil
}

// GetStatus returns the current update status
func (c *simpleInstaller) GetStatus() UpdateStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// IsRunning returns whether an update is currently running
func (c *simpleInstaller) IsRunning() bool {
	return atomic.LoadInt32(&c.running) == 1
}

// GetProgress returns the current progress information
func (c *simpleInstaller) GetProgress() Progress {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// For simple implementation, we don't have real progress
	// We can simulate some progress based on status
	progress := c.progress
	if c.status == StatusRun {
		progress.Status = StatusRun
		progress.Message = "Update in progress"
		// Simulate some progress
		if c.totalSteps > 0 {
			progress.TotalSteps = c.totalSteps
			progress.CurrentStep = c.currentStep
		}
	}

	return progress
}

// Subscribe returns a channel for receiving update events
func (c *simpleInstaller) Subscribe() (<-chan UpdateEvent, func()) {
	eventChan := make(chan UpdateEvent, 10)

	c.mu.Lock()
	c.subscribers = append(c.subscribers, eventChan)
	c.mu.Unlock()

	// Return unsubscribe function
	unsubscribe := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, ch := range c.subscribers {
			if ch == eventChan {
				c.subscribers = append(c.subscribers[:i], c.subscribers[i+1:]...)
				close(eventChan)
				break
			}
		}
	}

	return eventChan, unsubscribe
}

// WaitForCompletion waits for the update to complete
func (c *simpleInstaller) WaitForCompletion(timeout time.Duration) UpdateResult {
	if !c.IsRunning() {
		return UpdateResult{
			Success: false,
			Code:    -1,
			Message: "No update in progress",
		}
	}

	// Wait for completion or timeout
	select {
	case <-time.After(timeout):
		return UpdateResult{
			Success: false,
			Code:    -2,
			Message: "Timeout waiting for completion",
		}
	case <-c.closeChan:
		return UpdateResult{
			Success: false,
			Code:    -3,
			Message: "Client closed",
		}
	}
}

// Close shuts down the client and releases resources
func (c *simpleInstaller) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Cancel any running update
	if c.ctxCancel != nil {
		c.ctxCancel()
	}

	// Kill any running command
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil {
			// Process might have already finished
			log.Printf("Warning: failed to kill swupdate process: %v", err)
		}
	}

	// Clean up temp file
	if c.dataFile != nil {
		c.dataFile.Close()
		os.Remove(c.dataFile.Name())
		c.dataFile = nil
	}

	close(c.closeChan)
	<-c.closeDone

	// Close all subscriber channels
	for _, ch := range c.subscribers {
		close(ch)
	}
	c.subscribers = nil

	close(c.closeDone)
	return nil
}

// notifySubscribers sends an event to all subscribers
func (c *simpleInstaller) notifySubscribers(event UpdateEvent) {
	c.mu.RLock()
	subscribers := make([]chan UpdateEvent, len(c.subscribers))
	copy(subscribers, c.subscribers)
	c.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
			// Channel full, skip this event
			log.Printf("Warning: subscriber channel full, dropping event")
		}
	}
}

// SetBufferSize sets the buffer size for data transfers
func (c *simpleInstaller) SetBufferSize(size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bufferSize = size
}
