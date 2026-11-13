package swupdate_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dchauviere/speakerctld/internal/swupdate"
)

// TestNewInstaller tests creating a new installer
func TestNewInstaller(t *testing.T) {
	// Skip this test if SWUpdate is not available
	// This is just a basic compilation test
	installer, err := swupdate.NewInstaller()
	if err != nil {
		t.Skipf("SWUpdate not available: %v", err)
	}
	if installer == nil {
		t.Fatal("Expected non-nil installer")
	}
	installer.Close()
}

// TestNewInstallerWithPaths tests creating a new installer with custom paths
func TestNewInstallerWithPaths(t *testing.T) {
	installer, err := swupdate.NewInstallerWithPaths("/tmp/sockinstctrl", "/tmp/swupdateprog")
	if err != nil {
		t.Skipf("SWUpdate not available: %v", err)
	}
	if installer == nil {
		t.Fatal("Expected non-nil installer")
	}
	installer.Close()
}

// TestStatusString tests the string representation of status
func TestStatusString(t *testing.T) {
	tests := []struct {
		status swupdate.UpdateStatus
		want   string
	}{
		{swupdate.StatusIdle, "idle"},
		{swupdate.StatusStart, "start"},
		{swupdate.StatusRun, "run"},
		{swupdate.StatusSuccess, "success"},
		{swupdate.StatusFailure, "failure"},
		{swupdate.StatusDownload, "download"},
		{swupdate.StatusDone, "done"},
		{swupdate.StatusSubprocess, "subprocess"},
		{swupdate.StatusProgress, "progress"},
		{swupdate.UpdateStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Status(%d).String() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// TestSubscribe tests the subscription mechanism
func TestSubscribe(t *testing.T) {
	installer, err := swupdate.NewInstaller()
	if err != nil {
		t.Skipf("SWUpdate not available: %v", err)
	}
	defer installer.Close()

	eventChan, unsubscribe := installer.Subscribe()
	defer unsubscribe()

	// Check that channel is not nil
	if eventChan == nil {
		t.Fatal("Expected non-nil event channel")
	}

	// Check that we can receive from the channel (it might be empty)
	select {
	case <-eventChan:
		// Got an event
	case <-time.After(100 * time.Millisecond):
		// No event, which is fine
	}
}

// TestProgressStruct tests the Progress struct
func TestProgressStruct(t *testing.T) {
	progress := swupdate.Progress{
		Status:         swupdate.StatusRun,
		DownloadPercent: 50,
		DownloadBytes:  1024,
		TotalSteps:     10,
		CurrentStep:    5,
		StepPercent:    50,
		CurrentImage:   "rootfs",
		HandlerName:    "raw",
		Message:        "Installing",
		Source:         swupdate.SourceLocal,
	}

	// Just check that we can create and use the struct
	if progress.Status != swupdate.StatusRun {
		t.Errorf("Expected StatusRun, got %v", progress.Status)
	}
	if progress.DownloadPercent != 50 {
		t.Errorf("Expected DownloadPercent 50, got %v", progress.DownloadPercent)
	}
}

// TestUpdateEventStruct tests the UpdateEvent struct
func TestUpdateEventStruct(t *testing.T) {
	event := swupdate.UpdateEvent{
		Type: "progress",
		Status: swupdate.StatusRun,
	}

	if event.Type != "progress" {
		t.Errorf("Expected Type 'progress', got %v", event.Type)
	}
}

// Example_installer shows how to use the swupdate package
func Example_installer() {
	// Create a new installer
	installer, err := swupdate.NewInstaller()
	if err != nil {
		fmt.Printf("Failed to create installer: %v\n", err)
		return
	}
	defer installer.Close()

	// Subscribe to events
	eventChan, unsubscribe := installer.Subscribe()
	defer unsubscribe()

	// Start an update from a file
	source := swupdate.UpdateSource{
		Type:     swupdate.SourceLocal,
		FilePath: "/path/to/update.swu",
		DryRun:   false,
	}

	go func() {
		// Process events
		for event := range eventChan {
			fmt.Printf("Event: %s, Status: %s\n", event.Type, event.Status.String())
			if event.Progress != nil {
				fmt.Printf("  Progress: %d%%\n", event.Progress.DownloadPercent)
			}
		}
	}()

	// Start the update
	if err := installer.StartUpdate(source); err != nil {
		fmt.Printf("Failed to start update: %v\n", err)
		return
	}

	// Wait for completion
	result := installer.WaitForCompletion(30 * time.Minute)
	fmt.Printf("Update result: %v\n", result.Success)
}

// Example_installer_withReader shows how to use a reader as the source
func Example_installer_withReader() {
	// Create installer
	installer, err := swupdate.NewInstaller()
	if err != nil {
		fmt.Printf("Failed to create installer: %v\n", err)
		return
	}
	defer installer.Close()

	// Open a file to read from
	file, err := os.Open("/path/to/update.swu")
	if err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		return
	}
	defer file.Close()

	// Get file info
	info, err := file.Stat()
	if err != nil {
		fmt.Printf("Failed to stat file: %v\n", err)
		return
	}

	// Start update from reader
	source := swupdate.UpdateSource{
		Type:       swupdate.SourceLocal,
		Reader:     file,
		FileSize:   info.Size(),
		BufferSize: 131072, // 128KB
	}

	if err := installer.StartUpdate(source); err != nil {
		fmt.Printf("Failed to start update: %v\n", err)
		return
	}

	// Wait for completion
	result := installer.WaitForCompletion(30 * time.Minute)
	fmt.Printf("Update result: %v\n", result.Success)
}

// BenchmarkProgressNotification benchmarks progress notification performance
// Note: This benchmark requires CGO to be enabled
func BenchmarkProgressNotification(b *testing.B) {
	installer, err := swupdate.NewInstaller()
	if err != nil {
		b.Skipf("SWUpdate not available: %v", err)
	}
	defer installer.Close()

	// Create a regular channel for testing
	testChan := make(chan swupdate.UpdateEvent, 10)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		select {
		case testChan <- swupdate.UpdateEvent{
			Type:   "progress",
			Status: swupdate.StatusRun,
		}:
		default:
		}
	}
	
	close(testChan)
}
