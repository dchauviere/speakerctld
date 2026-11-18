package swupdate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// progressGracePeriod is how long swupdate-progress may keep draining output
// after swupdate has exited.
const progressGracePeriod = 2 * time.Second

// simpleInstaller implements the Installer interface by running the
// swupdate command and following its progress with swupdate-progress.
type simpleInstaller struct {
	mu          sync.RWMutex
	running     int32 // atomic for thread safety
	cancelled   int32 // atomic, set by CancelUpdate
	status      UpdateStatus
	progress    Progress
	subscribers []chan UpdateEvent
	closeChan   chan struct{}
	cmd         *exec.Cmd
	progressCmd *exec.Cmd
	ctx         context.Context
	ctxCancel   context.CancelFunc
	dataFile    *os.File
	bufferSize  int
}

// NewInstaller creates a new SWUpdate installer
func NewInstaller() (Installer, error) {
	return NewInstallerWithPaths("", "")
}

// NewInstallerWithPaths creates a new SWUpdate installer (paths are ignored)
func NewInstallerWithPaths(ctrlSocketPath, progSocketPath string) (Installer, error) {
	if _, err := exec.LookPath("swupdate"); err != nil {
		return nil, fmt.Errorf("swupdate command not found in PATH: %w", err)
	}
	if _, err := exec.LookPath("swupdate-progress"); err != nil {
		log.Printf("Warning: swupdate-progress not found in PATH, progress will not be reported: %v", err)
	}

	return &simpleInstaller{
		status:     StatusIdle,
		closeChan:  make(chan struct{}),
		bufferSize: DefaultBufferSize,
	}, nil
}

// StartUpdate starts a new update with the given source
func (c *simpleInstaller) StartUpdate(source UpdateSource) error {
	if !atomic.CompareAndSwapInt32(&c.running, 0, 1) {
		return ErrUpdateInProgress
	}

	if source.Reader == nil && source.FilePath == "" {
		atomic.StoreInt32(&c.running, 0)
		return ErrNoUpdateFile
	}

	atomic.StoreInt32(&c.cancelled, 0)
	c.mu.Lock()
	c.status = StatusStart
	c.progress = Progress{Status: StatusStart}
	c.mu.Unlock()
	c.notifySubscribers(UpdateEvent{Type: "started", Status: StatusStart})

	go c.startUpdateInternal(source)
	return nil
}

// fail records a failure and notifies subscribers.
func (c *simpleInstaller) fail(err error) {
	c.mu.Lock()
	c.status = StatusFailure
	c.progress.Status = StatusFailure
	c.progress.Message = err.Error()
	c.mu.Unlock()
	c.notifySubscribers(UpdateEvent{
		Type:    "failed",
		Status:  StatusFailure,
		Error:   err,
		Message: err.Error(),
	})
}

// saveReader copies the reader to a temporary .swu file.
func (c *simpleInstaller) saveReader(src interface{}) (string, error) {
	reader, ok := src.(io.Reader)
	if !ok {
		return "", errors.New("reader must implement io.Reader")
	}
	tempFile, err := os.CreateTemp("", "swupdate-*.swu")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	if _, err := io.CopyBuffer(tempFile, reader, make([]byte, c.bufferSize)); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to save update data: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}
	c.mu.Lock()
	c.dataFile = tempFile
	c.mu.Unlock()
	return tempFile.Name(), nil
}

// startUpdateInternal runs `swupdate -i <file>` and follows swupdate-progress.
func (c *simpleInstaller) startUpdateInternal(source UpdateSource) {
	defer atomic.StoreInt32(&c.running, 0)

	swuFilePath := source.FilePath
	if swuFilePath == "" {
		path, err := c.saveReader(source.Reader)
		if err != nil {
			c.fail(err)
			return
		}
		swuFilePath = path
		defer func() {
			c.mu.Lock()
			os.Remove(path)
			c.dataFile = nil
			c.mu.Unlock()
		}()
	}

	if info, err := os.Stat(swuFilePath); err != nil {
		log.Printf("Warning: cannot stat SWU file %s: %v", swuFilePath, err)
	} else {
		log.Printf("SWUpdate: starting with file %s, size: %d bytes", swuFilePath, info.Size())
	}

	args := []string{"-i", swuFilePath}
	if source.DryRun {
		args = append(args, "-n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "swupdate", args...)
	cmd.Stdout = logWriter{prefix: "swupdate"}
	cmd.Stderr = logWriter{prefix: "swupdate"}
	cmd.WaitDelay = time.Second

	c.mu.Lock()
	c.ctx, c.ctxCancel = ctx, cancel
	c.cmd = cmd
	c.status = StatusRun
	c.mu.Unlock()
	c.notifySubscribers(UpdateEvent{
		Type:    "status",
		Status:  StatusRun,
		Message: fmt.Sprintf("Starting swupdate with file: %s", swuFilePath),
	})

	if err := cmd.Start(); err != nil {
		c.fail(fmt.Errorf("failed to start swupdate: %w", err))
		return
	}

	progressDone, outcome := c.followProgress(ctx)

	waitErr := cmd.Wait()

	// Let swupdate-progress drain its last lines, then stop it.
	select {
	case <-progressDone:
	case <-time.After(progressGracePeriod):
	}
	cancel()
	<-progressDone

	if atomic.LoadInt32(&c.cancelled) == 1 {
		return
	}

	switch {
	case waitErr != nil:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			c.fail(fmt.Errorf("swupdate failed with exit code %d", exitErr.ExitCode()))
		} else {
			c.fail(fmt.Errorf("swupdate failed: %w", waitErr))
		}
	case atomic.LoadInt32(outcome) == int32(outcomeFailure):
		c.fail(errors.New("swupdate reported a failure"))
	default:
		c.mu.Lock()
		c.status = StatusSuccess
		c.progress.Status = StatusSuccess
		c.progress.StepPercent = 100
		c.progress.Message = "Update completed successfully"
		c.mu.Unlock()
		c.notifySubscribers(UpdateEvent{
			Type:    "completed",
			Status:  StatusSuccess,
			Message: "Update completed successfully",
		})
	}
}

// followProgress runs swupdate-progress and publishes the progress it reports.
// The returned channel is closed once the output has been fully consumed; the
// returned value holds the terminal outcome seen in the output.
func (c *simpleInstaller) followProgress(ctx context.Context) (<-chan struct{}, *int32) {
	done := make(chan struct{})
	outcome := new(int32)

	path, err := exec.LookPath("swupdate-progress")
	if err != nil {
		close(done)
		return done, outcome
	}

	// Line-buffer the output so progress is reported while the update runs.
	name, args := path, []string{}
	if stdbuf, err := exec.LookPath("stdbuf"); err == nil {
		name, args = stdbuf, []string{"-oL", "-eL", path}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = logWriter{prefix: "swupdate-progress"}
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Warning: swupdate-progress stdout: %v", err)
		close(done)
		return done, outcome
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Warning: failed to start swupdate-progress: %v", err)
		close(done)
		return done, outcome
	}
	c.mu.Lock()
	c.progressCmd = cmd
	c.mu.Unlock()

	// Closing the pipe unblocks the scanner if a child process keeps it open.
	go func() {
		<-ctx.Done()
		stdout.Close()
	}()

	go func() {
		defer close(done)
		scanner := newProgressScanner(bufio.NewScanner(stdout))
		for scanner.Scan() {
			c.handleProgressLine(scanner.Text(), outcome)
		}
		cmd.Wait()
	}()
	return done, outcome
}

func (c *simpleInstaller) handleProgressLine(line string, outcome *int32) {
	c.mu.Lock()
	p := c.progress
	changed, out := parseProgressLine(line, &p)
	if !changed {
		c.mu.Unlock()
		return
	}
	if out == outcomeFailure {
		atomic.StoreInt32(outcome, int32(outcomeFailure))
	} else if out == outcomeSuccess {
		// Success is only final once swupdate itself exits cleanly.
		p.Status = StatusRun
		atomic.CompareAndSwapInt32(outcome, int32(outcomeNone), int32(outcomeSuccess))
	}
	p.Source = SourceLocal
	c.progress = p
	c.mu.Unlock()
	log.Printf("swupdate-progress: %s", line)
	if out == outcomeFailure {
		return
	}
	c.notifySubscribers(UpdateEvent{Type: "progress", Status: StatusRun, Progress: &p})
}

// logWriter forwards process output to the log.
type logWriter struct{ prefix string }

func (w logWriter) Write(b []byte) (int, error) {
	log.Printf("%s: %s", w.prefix, string(bytes.TrimRight(b, "\r\n")))
	return len(b), nil
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

	if !c.IsRunning() {
		c.mu.Unlock()
		return nil
	}

	atomic.StoreInt32(&c.cancelled, 1)
	if c.ctxCancel != nil {
		c.ctxCancel()
	}

	// Try to kill the command
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil {
			// Process might have already finished
			c.mu.Unlock()
			return nil
		}
	}

	c.status = StatusIdle
	c.mu.Unlock()
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

	progress := c.progress
	return progress
}

// Subscribe returns a channel for receiving update events
func (c *simpleInstaller) Subscribe() (<-chan UpdateEvent, func()) {
	eventChan := make(chan UpdateEvent, 10)

	c.mu.Lock()
	select {
	case <-c.closeChan:
		close(eventChan)
	default:
		c.subscribers = append(c.subscribers, eventChan)
	}
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

	// Close all subscriber channels
	for _, ch := range c.subscribers {
		close(ch)
	}
	c.subscribers = nil

	return nil
}

// notifySubscribers sends an event to all subscribers
func (c *simpleInstaller) notifySubscribers(event UpdateEvent) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, ch := range c.subscribers {
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
