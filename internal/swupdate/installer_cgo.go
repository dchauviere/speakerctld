//go:build cgo
// +build cgo

package swupdate

/*
#cgo pkg-config: swupdate
#cgo CFLAGS: -I${SRCDIR}/c_wrapper
#cgo SOURCES: ${SRCDIR}/c_wrapper/swupdate_wrapper.c

#include <stdlib.h>
#include "swupdate_wrapper.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// swupdateClient implements the Installer interface with CGO
type swupdateClient struct {
	mu             sync.RWMutex
	initialized    bool
	running        int32 // atomic for thread safety
	status         UpdateStatus
	progress       Progress
	subscribers    []chan UpdateEvent
	closeChan      chan struct{}
	closeDone      chan struct{}
	ctrlSocketPath string
	progSocketPath string
	dataReader     interface{}
	dataFile       *os.File
	bufferSize     int
	bytesSent      int64
}

// NewInstaller creates a new SWUpdate installer client
func NewInstaller() (Installer, error) {
	return NewInstallerWithPaths("/tmp/sockinstctrl", "/tmp/swupdateprog")
}

// NewInstallerWithPaths creates a new SWUpdate installer with custom socket paths
func NewInstallerWithPaths(ctrlSocketPath, progSocketPath string) (Installer, error) {
	client := &swupdateClient{
		initialized:    false,
		running:        0,
		status:         StatusIdle,
		closeChan:      make(chan struct{}),
		closeDone:      make(chan struct{}),
		bufferSize:     DefaultBufferSize,
		ctrlSocketPath: ctrlSocketPath,
		progSocketPath: progSocketPath,
	}

	// Initialize the C wrapper
	ctrlPath := C.CString(client.ctrlSocketPath)
	progPath := C.CString(client.progSocketPath)
	defer C.free(unsafe.Pointer(ctrlPath))
	defer C.free(unsafe.Pointer(progPath))

	result := C.swupdate_wrapper_init(ctrlPath, progPath)
	if result != 0 {
		return nil, fmt.Errorf("failed to initialize SWUpdate wrapper: %d", result)
	}

	client.initialized = true
	client.status = StatusIdle

	// Start progress monitoring goroutine
	go client.monitorProgress()

	return client, nil
}

// StartUpdate starts a new update with the given source
func (c *swupdateClient) StartUpdate(source UpdateSource) error {
	if !c.initialized {
		return ErrNotInitialized
	}

	// Check if already running
	if atomic.CompareAndSwapInt32(&c.running, 0, 1) == false {
		return ErrUpdateInProgress
	}
	defer func() {
		if r := recover(); r != nil {
			atomic.StoreInt32(&c.running, 0)
			panic(r)
		}
	}()

	// Validate source
	if source.Reader == nil && source.FilePath == "" {
		atomic.StoreInt32(&c.running, 0)
		return ErrNoUpdateFile
	}

	// If we have a file path, open it
	if source.FilePath != "" {
		file, err := os.Open(source.FilePath)
		if err != nil {
			atomic.StoreInt32(&c.running, 0)
			return fmt.Errorf("failed to open update file: %w", err)
		}
		c.dataFile = file
		c.dataReader = file
		c.bytesSent = 0
	}

	if source.Reader != nil {
		// Check if it's an io.Reader
		if reader, ok := source.Reader.(io.Reader); ok {
			c.dataReader = reader
			c.bytesSent = 0
		} else {
			atomic.StoreInt32(&c.running, 0)
			return fmt.Errorf("reader must implement io.Reader")
		}
	}

	// Set buffer size if specified
	if source.BufferSize > 0 {
		c.bufferSize = source.BufferSize
	}

	// Start the update in a goroutine
	go c.startUpdateInternal(source)

	c.mu.Lock()
	c.status = StatusStart
	c.mu.Unlock()
	c.notifySubscribers(UpdateEvent{
		Type:   "started",
		Status: StatusStart,
	})

	return nil
}

// startUpdateInternal does the actual update work
func (c *swupdateClient) startUpdateInternal(source UpdateSource) {
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

	// Start the update
	var softwareSetC, runningModeC *C.char
	if source.SoftwareSet != "" {
		softwareSetC = C.CString(source.SoftwareSet)
		defer C.free(unsafe.Pointer(softwareSetC))
	}
	if source.RunningMode != "" {
		runningModeC = C.CString(source.RunningMode)
		defer C.free(unsafe.Pointer(runningModeC))
	}

	result := C.swupdate_wrapper_start_update(
		C.UpdateSourceType(source.Type),
		C.bool(source.DryRun),
		softwareSetC,
		runningModeC,
	)

	if result != 0 {
		c.mu.Lock()
		c.status = StatusFailure
		c.mu.Unlock()
		c.notifySubscribers(UpdateEvent{
			Type:    "failed",
			Status:  StatusFailure,
			Error:   fmt.Errorf("failed to start update: %d", result),
			Message: fmt.Sprintf("SWUpdate start failed with code %d", result),
		})
		return
	}

	// Stream data to SWUpdate
	if c.dataReader != nil {
		if reader, ok := c.dataReader.(io.Reader); ok {
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
					return
				}

				if n == 0 {
					// EOF - we're done sending data
					break
				}

				// Send data to SWUpdate
				result := C.swupdate_wrapper_send_data((*C.char)(unsafe.Pointer(&buf[0])), C.int(n))
				if result < 0 {
					c.mu.Lock()
					c.status = StatusFailure
					c.mu.Unlock()
					c.notifySubscribers(UpdateEvent{
						Type:    "failed",
						Status:  StatusFailure,
						Error:   fmt.Errorf("failed to send data to SWUpdate"),
						Message: "Data send failed",
					})
					return
				}

				c.bytesSent += int64(n)
			}
		}
	}

	// Wait for completion
	c.mu.Lock()
	c.status = StatusSuccess
	c.mu.Unlock()
	c.notifySubscribers(UpdateEvent{
		Type:    "completed",
		Status:  StatusSuccess,
		Message: "Update completed successfully",
	})
}

// SendData sends a chunk of data to SWUpdate
func (c *swupdateClient) SendData(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized || !c.IsRunning() {
		return 0, ErrNotInitialized
	}

	// Call the C wrapper to send data
	result := C.swupdate_wrapper_send_data(
		(*C.char)(unsafe.Pointer(&data[0])),
		C.int(len(data)),
	)

	if result < 0 {
		return 0, fmt.Errorf("failed to send data: %d", result)
	}

	return int(result), nil
}

// CancelUpdate attempts to cancel the current update
func (c *swupdateClient) CancelUpdate() error {
	// SWUpdate IPC doesn't support cancel in the current version
	return ErrCancelNotSupported
}

// GetStatus returns the current update status
func (c *swupdateClient) GetStatus() UpdateStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.initialized {
		return StatusIdle
	}

	// Get status from C wrapper
	status := C.swupdate_wrapper_get_status()
	return UpdateStatus(status)
}

// IsRunning returns whether an update is currently running
func (c *swupdateClient) IsRunning() bool {
	return atomic.LoadInt32(&c.running) == 1
}

// GetProgress returns the current progress information
func (c *swupdateClient) GetProgress() Progress {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.progress
}

// Subscribe returns a channel for receiving update events
func (c *swupdateClient) Subscribe() (<-chan UpdateEvent, func()) {
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
func (c *swupdateClient) WaitForCompletion(timeout time.Duration) UpdateResult {
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
func (c *swupdateClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	close(c.closeChan)
	<-c.closeDone

	// Close data file if open
	if c.dataFile != nil {
		c.dataFile.Close()
		c.dataFile = nil
	}
	c.dataReader = nil

	// Cleanup C wrapper
	C.swupdate_wrapper_cleanup()

	c.initialized = false

	// Close all subscriber channels
	for _, ch := range c.subscribers {
		close(ch)
	}
	c.subscribers = nil

	close(c.closeDone)
	return nil
}

// SetCtrlSocketPath sets the path to the SWUpdate control socket
func (c *swupdateClient) SetCtrlSocketPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctrlSocketPath = path
}

// SetProgSocketPath sets the path to the SWUpdate progress socket
func (c *swupdateClient) SetProgSocketPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progSocketPath = path
}

// SetBufferSize sets the buffer size for data transfers
func (c *swupdateClient) SetBufferSize(size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bufferSize = size
}

// monitorProgress monitors the progress socket for updates
func (c *swupdateClient) monitorProgress() {
	defer close(c.closeDone)

	for {
		select {
		case <-c.closeChan:
			return
		case <-time.After(100 * time.Millisecond):
			c.checkProgress()
		}
	}
}

// checkProgress checks for progress updates from the socket
func (c *swupdateClient) checkProgress() {
	if !c.IsRunning() {
		return
	}

	// Try to read progress from socket
	var cProgress C.SwupdateProgress
	result := C.swupdate_progress_receive(&cProgress)

	if result != 0 {
		// Failed to read progress, continue
		return
	}

	// Convert C progress to Go progress
	progress := Progress{
		Status:          UpdateStatus(cProgress.status),
		DownloadPercent: int(cProgress.dwl_percent),
		DownloadBytes:   int64(cProgress.dwl_bytes),
		TotalSteps:      int(cProgress.total_steps),
		CurrentStep:     int(cProgress.current_step),
		StepPercent:     int(cProgress.step_percent),
		CurrentImage:    C.GoString(&cProgress.current_image[0]),
		HandlerName:     C.GoString(&cProgress.handler_name[0]),
		Message:         C.GoString(&cProgress.message[0]),
		Source:          int(cProgress.source),
	}

	c.mu.Lock()
	c.progress = progress
	c.mu.Unlock()

	c.notifySubscribers(UpdateEvent{
		Type:     "progress",
		Status:   progress.Status,
		Progress: &progress,
	})
}

// notifySubscribers sends an event to all subscribers
func (c *swupdateClient) notifySubscribers(event UpdateEvent) {
	c.mu.RLock()
	subscribers := c.subscribers
	c.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
			// Channel full, skip this event
		}
	}
}
