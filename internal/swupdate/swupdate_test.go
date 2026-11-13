package swupdate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

// mockReader implements io.Reader for testing
type mockReader struct {
	data    []byte
	pos     int
	readErr error
}

func (r *mockReader) Read(p []byte) (n int, err error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	
	n = copy(p, r.data[r.pos:])
	r.pos += n
	
	return n, nil
}

// TestUpdateStatusString tests the String method of UpdateStatus
func TestUpdateStatusString(t *testing.T) {
	tests := []struct {
		status UpdateStatus
		want   string
	}{
		{StatusIdle, "idle"},
		{StatusStart, "start"},
		{StatusRun, "run"},
		{StatusSuccess, "success"},
		{StatusFailure, "failure"},
		{StatusDownload, "download"},
		{StatusDone, "done"},
		{StatusSubprocess, "subprocess"},
		{StatusProgress, "progress"},
		{UpdateStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Status(%d).String() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// TestProgressStruct tests the Progress struct
func TestProgressStruct(t *testing.T) {
	progress := Progress{
		Status:         StatusRun,
		DownloadPercent: 50,
		DownloadBytes:  1024,
		TotalSteps:     10,
		CurrentStep:    5,
		StepPercent:    50,
		CurrentImage:   "rootfs",
		HandlerName:    "raw",
		Message:        "Installing",
		Source:         SourceLocal,
	}

	// Test all fields
	if progress.Status != StatusRun {
		t.Errorf("Expected StatusRun, got %v", progress.Status)
	}
	if progress.DownloadPercent != 50 {
		t.Errorf("Expected DownloadPercent 50, got %v", progress.DownloadPercent)
	}
	if progress.DownloadBytes != 1024 {
		t.Errorf("Expected DownloadBytes 1024, got %v", progress.DownloadBytes)
	}
	if progress.TotalSteps != 10 {
		t.Errorf("Expected TotalSteps 10, got %v", progress.TotalSteps)
	}
	if progress.CurrentStep != 5 {
		t.Errorf("Expected CurrentStep 5, got %v", progress.CurrentStep)
	}
	if progress.StepPercent != 50 {
		t.Errorf("Expected StepPercent 50, got %v", progress.StepPercent)
	}
	if progress.CurrentImage != "rootfs" {
		t.Errorf("Expected CurrentImage 'rootfs', got %v", progress.CurrentImage)
	}
	if progress.HandlerName != "raw" {
		t.Errorf("Expected HandlerName 'raw', got %v", progress.HandlerName)
	}
	if progress.Message != "Installing" {
		t.Errorf("Expected Message 'Installing', got %v", progress.Message)
	}
	if progress.Source != SourceLocal {
		t.Errorf("Expected Source SourceLocal, got %v", progress.Source)
	}
}

// TestUpdateEventStruct tests the UpdateEvent struct
func TestUpdateEventStruct(t *testing.T) {
	progress := &Progress{
		Status: StatusRun,
	}

	event := UpdateEvent{
		Type:     "progress",
		Status:   StatusRun,
		Progress: progress,
		Error:    nil,
		Message:  "Test message",
	}

	if event.Type != "progress" {
		t.Errorf("Expected Type 'progress', got %v", event.Type)
	}
	if event.Status != StatusRun {
		t.Errorf("Expected Status StatusRun, got %v", event.Status)
	}
	if event.Progress != progress {
		t.Errorf("Expected Progress to be set")
	}
	if event.Error != nil {
		t.Errorf("Expected Error to be nil")
	}
	if event.Message != "Test message" {
		t.Errorf("Expected Message 'Test message', got %v", event.Message)
	}
}

// TestUpdateSourceStruct tests the UpdateSource struct
func TestUpdateSourceStruct(t *testing.T) {
	reader := &mockReader{data: []byte("test")}

	source := UpdateSource{
		Type:         SourceLocal,
		FilePath:     "/path/to/file.swu",
		Reader:       reader,
		FileSize:     1024,
		SoftwareSet:  "stable",
		RunningMode:  "production",
		DryRun:       true,
		BufferSize:   131072,
	}

	if source.Type != SourceLocal {
		t.Errorf("Expected Type SourceLocal, got %v", source.Type)
	}
	if source.FilePath != "/path/to/file.swu" {
		t.Errorf("Expected FilePath '/path/to/file.swu', got %v", source.FilePath)
	}
	if source.Reader != reader {
		t.Errorf("Expected Reader to be set")
	}
	if source.FileSize != 1024 {
		t.Errorf("Expected FileSize 1024, got %v", source.FileSize)
	}
	if source.SoftwareSet != "stable" {
		t.Errorf("Expected SoftwareSet 'stable', got %v", source.SoftwareSet)
	}
	if source.RunningMode != "production" {
		t.Errorf("Expected RunningMode 'production', got %v", source.RunningMode)
	}
	if source.DryRun != true {
		t.Errorf("Expected DryRun true, got %v", source.DryRun)
	}
	if source.BufferSize != 131072 {
		t.Errorf("Expected BufferSize 131072, got %v", source.BufferSize)
	}
}

// TestUpdateResultStruct tests the UpdateResult struct
func TestUpdateResultStruct(t *testing.T) {
	result := UpdateResult{
		Success: true,
		Code:    0,
		Message: "Success",
	}

	if result.Success != true {
		t.Errorf("Expected Success true, got %v", result.Success)
	}
	if result.Code != 0 {
		t.Errorf("Expected Code 0, got %v", result.Code)
	}
	if result.Message != "Success" {
		t.Errorf("Expected Message 'Success', got %v", result.Message)
	}
}

// TestMockReader tests the mock reader
func TestMockReader(t *testing.T) {
	data := []byte("Hello, World!")
	reader := &mockReader{data: data}

	// Test reading all data
	buf := make([]byte, 100)
	n, err := reader.Read(buf)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if n != len(data) {
		t.Errorf("Expected to read %d bytes, got %d", len(data), n)
	}
	if string(buf[:n]) != string(data) {
		t.Errorf("Expected '%s', got '%s'", string(data), string(buf[:n]))
	}

	// Test reading past end
	reader.pos = len(data)
	n, err = reader.Read(buf)
	if err != io.EOF {
		t.Errorf("Expected io.EOF, got %v", err)
	}
	if n != 0 {
		t.Errorf("Expected 0 bytes, got %d", n)
	}

	// Test read error
	reader.pos = 0
	reader.readErr = errors.New("read error")
	_, err = reader.Read(buf)
	if err == nil || err.Error() != "read error" {
		t.Errorf("Expected read error, got %v", err)
	}
}

// TestBufferSize tests the default buffer size constant
func TestBufferSize(t *testing.T) {
	if DefaultBufferSize != 131072 {
		t.Errorf("Expected DefaultBufferSize 131072, got %v", DefaultBufferSize)
	}
}

// TestSourceConstants tests the source type constants
func TestSourceConstants(t *testing.T) {
	if SourceUnknown != 0 {
		t.Errorf("Expected SourceUnknown 0, got %v", SourceUnknown)
	}
	if SourceWebserver != 1 {
		t.Errorf("Expected SourceWebserver 1, got %v", SourceWebserver)
	}
	if SourceSuricatta != 2 {
		t.Errorf("Expected SourceSuricatta 2, got %v", SourceSuricatta)
	}
	if SourceDownloader != 3 {
		t.Errorf("Expected SourceDownloader 3, got %v", SourceDownloader)
	}
	if SourceLocal != 4 {
		t.Errorf("Expected SourceLocal 4, got %v", SourceLocal)
	}
	if SourceChunksDownloader != 5 {
		t.Errorf("Expected SourceChunksDownloader 5, got %v", SourceChunksDownloader)
	}
}

// TestErrorVariables tests the error variables
func TestErrorVariables(t *testing.T) {
	if ErrUpdateInProgress == nil {
		t.Error("Expected ErrUpdateInProgress to be set")
	}
	if ErrNotInitialized == nil {
		t.Error("Expected ErrNotInitialized to be set")
	}
	if ErrCancelNotSupported == nil {
		t.Error("Expected ErrCancelNotSupported to be set")
	}
	if ErrNoUpdateFile == nil {
		t.Error("Expected ErrNoUpdateFile to be set")
	}
}

// TestEventChannel tests event channel creation
func TestEventChannel(t *testing.T) {
	// Create a simple channel
	eventChan := make(chan UpdateEvent, 10)

	// Test sending and receiving an event
	event := UpdateEvent{
		Type:   "test",
		Status: StatusIdle,
	}

	eventChan <- event

	select {
	case received := <-eventChan:
		if received.Type != "test" {
			t.Errorf("Expected Type 'test', got %v", received.Type)
		}
		if received.Status != StatusIdle {
			t.Errorf("Expected Status StatusIdle, got %v", received.Status)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}
}

// TestProgressJSON tests JSON serialization of Progress
func TestProgressJSON(t *testing.T) {
	progress := Progress{
		Status:         StatusRun,
		DownloadPercent: 50,
		DownloadBytes:  1024,
		TotalSteps:     10,
		CurrentStep:    5,
		StepPercent:    50,
		CurrentImage:   "rootfs",
		HandlerName:    "raw",
		Message:        "Installing",
		Source:         SourceLocal,
	}

	// Marshal to JSON
	data, err := json.Marshal(progress)
	if err != nil {
		t.Fatalf("Failed to marshal progress: %v", err)
	}

	// Unmarshal back
	var decoded Progress
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal progress: %v", err)
	}

	// Verify fields
	if decoded.Status != StatusRun {
		t.Errorf("Expected Status StatusRun, got %v", decoded.Status)
	}
	if decoded.DownloadPercent != 50 {
		t.Errorf("Expected DownloadPercent 50, got %v", decoded.DownloadPercent)
	}
	if decoded.CurrentImage != "rootfs" {
		t.Errorf("Expected CurrentImage 'rootfs', got %v", decoded.CurrentImage)
	}
}

// TestUpdateEventJSON tests JSON serialization of UpdateEvent
func TestUpdateEventJSON(t *testing.T) {
	progress := &Progress{
		Status: StatusRun,
	}

	event := UpdateEvent{
		Type:     "progress",
		Status:   StatusRun,
		Progress: progress,
		Message:  "Test",
	}

	// Marshal to JSON
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	// Check that it contains expected fields
	if !bytes.Contains(data, []byte(`"type":"progress"`)) {
		t.Errorf("Expected JSON to contain type field")
	}
	// Status is marshaled as integer, not string
	if !bytes.Contains(data, []byte(`"status":2`)) { // StatusRun = 2
		t.Errorf("Expected JSON to contain status field, got: %s", string(data))
	}
	if !bytes.Contains(data, []byte(`"message":"Test"`)) {
		t.Errorf("Expected JSON to contain message field")
	}
}

// TestReaderWithData tests reading from a reader with data
func TestReaderWithData(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	reader := &mockReader{data: data}

	// Read in chunks
	chunk1 := make([]byte, 3)
	n1, err := reader.Read(chunk1)
	if err != nil {
		t.Fatalf("Failed to read chunk 1: %v", err)
	}
	if n1 != 3 {
		t.Errorf("Expected to read 3 bytes, got %d", n1)
	}
	if !bytes.Equal(chunk1, []byte{1, 2, 3}) {
		t.Errorf("Expected [1,2,3], got %v", chunk1)
	}

	chunk2 := make([]byte, 10)
	n2, err := reader.Read(chunk2)
	if err != nil {
		t.Fatalf("Failed to read chunk 2: %v", err)
	}
	if n2 != 7 {
		t.Errorf("Expected to read 7 bytes, got %d", n2)
	}
	if !bytes.Equal(chunk2[:n2], []byte{4, 5, 6, 7, 8, 9, 10}) {
		t.Errorf("Expected [4,5,6,7,8,9,10], got %v", chunk2[:n2])
	}

	// Next read should return EOF
	chunk3 := make([]byte, 10)
	n3, err := reader.Read(chunk3)
	if err != io.EOF {
		t.Errorf("Expected io.EOF, got %v", err)
	}
	if n3 != 0 {
		t.Errorf("Expected 0 bytes, got %d", n3)
	}
}

// BenchmarkProgressCreation benchmarks Progress struct creation
func BenchmarkProgressCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Progress{
			Status:         StatusRun,
			DownloadPercent: 50,
			DownloadBytes:  1024,
			TotalSteps:     10,
			CurrentStep:    5,
			StepPercent:    50,
			CurrentImage:   "rootfs",
			HandlerName:    "raw",
			Message:        "Installing",
			Source:         SourceLocal,
		}
	}
}

// BenchmarkUpdateEventCreation benchmarks UpdateEvent struct creation
func BenchmarkUpdateEventCreation(b *testing.B) {
	progress := &Progress{
		Status: StatusRun,
	}

	for i := 0; i < b.N; i++ {
		_ = UpdateEvent{
			Type:     "progress",
			Status:   StatusRun,
			Progress: progress,
			Message:  "Test",
		}
	}
}

// BenchmarkStatusString benchmarks status string conversion
func BenchmarkStatusString(b *testing.B) {
	statuses := []UpdateStatus{
		StatusIdle,
		StatusStart,
		StatusRun,
		StatusSuccess,
		StatusFailure,
		StatusDownload,
		StatusDone,
		StatusSubprocess,
		StatusProgress,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, status := range statuses {
			_ = status.String()
		}
	}
}
