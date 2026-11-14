package firmware

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/swupdate"
)

// MockStateMachine implements state.StateMachine for testing
type MockStateMachine struct{}

func (m *MockStateMachine) CurrentState() state.State {
	return state.Online
}

func (m *MockStateMachine) Transition(s state.State) error {
	return nil
}

func (m *MockStateMachine) IsProvisioned() bool {
	return true
}

func (m *MockStateMachine) SetProvisioned() error {
	return nil
}

func (m *MockStateMachine) WifiFailureCount() int {
	return 0
}

func (m *MockStateMachine) ResetWifiFailures() error {
	return nil
}

func (m *MockStateMachine) IncrementWifiFailures() error {
	return nil
}

func (m *MockStateMachine) CanTransitionTo(s state.State) bool {
	return true
}

// MockInstaller implements swupdate.Installer for testing
type MockInstaller struct{}

func (m *MockInstaller) StartUpdate(source swupdate.UpdateSource) error {
	return nil
}

func (m *MockInstaller) SendData(data []byte) (int, error) {
	return len(data), nil
}

func (m *MockInstaller) CancelUpdate() error {
	return nil
}

func (m *MockInstaller) GetStatus() swupdate.UpdateStatus {
	return swupdate.StatusIdle
}

func (m *MockInstaller) GetProgress() swupdate.Progress {
	return swupdate.Progress{}
}

func (m *MockInstaller) IsRunning() bool {
	return false
}

func (m *MockInstaller) Subscribe() (<-chan swupdate.UpdateEvent, func()) {
	ch := make(chan swupdate.UpdateEvent)
	return ch, func() { close(ch) }
}

func (m *MockInstaller) WaitForCompletion(timeout time.Duration) swupdate.UpdateResult {
	return swupdate.UpdateResult{}
}

func (m *MockInstaller) Close() error {
	return nil
}

func TestHandleFirmwareUpload_MultipartForm(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "firmware-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create handler
	mockStateMachine := &MockStateMachine{}
	mockInstaller := &MockInstaller{}
	
	handler, err := NewHandler(mockStateMachine, mockInstaller, tempDir)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Set a smaller max file size for testing
	handler.SetMaxFileSize(100 * 1024 * 1024) // 100MB

	// Create a test file (small SWU-like file)
	testFileContent := []byte("SWU\n\x00\x00\x00") // Simulate SWU magic
	testFilePath := filepath.Join(tempDir, "test.swu")
	if err := os.WriteFile(testFilePath, testFileContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Read test file
	testFileData, err := os.ReadFile(testFilePath)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	
	// Add firmware file
	part, err := writer.CreateFormFile("firmware", "test.swu")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	
	if _, err := part.Write(testFileData); err != nil {
		t.Fatalf("Failed to write file data: %v", err)
	}
	
	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Create request
	req := httptest.NewRequest("POST", "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Create response recorder
	rr := httptest.NewRecorder()

	// Serve request
	handler.ServeHTTP(rr, req)

	// Check response
	if rr.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d: %s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
}

func TestParseMultipartForm_MemoryLimit(t *testing.T) {
	// This test verifies that ParseMultipartForm is called with the correct memory limit
	// We can't directly test this, but we can verify that the handler uses maxFileSize
	
	tempDir, err := os.MkdirTemp("", "firmware-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockStateMachine := &MockStateMachine{}
	mockInstaller := &MockInstaller{}
	
	handler, err := NewHandlerWithOptions(mockStateMachine, mockInstaller, tempDir, HandlerOptions{
		MaxFileSize: 200 * 1024 * 1024, // 200MB
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Verify maxFileSize is set correctly
	if handler.maxFileSize != 200*1024*1024 {
		t.Errorf("Expected maxFileSize to be 200MB, got %d", handler.maxFileSize)
	}
}

func TestHandleFirmwareUpload_FileTooLarge(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "firmware-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockStateMachine := &MockStateMachine{}
	mockInstaller := &MockInstaller{}
	
	handler, err := NewHandler(mockStateMachine, mockInstaller, tempDir)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Set a small max file size for testing
	handler.SetMaxFileSize(100) // 100 bytes

	// Create a test file larger than max
	largeFileContent := make([]byte, 200)
	for i := range largeFileContent {
		largeFileContent[i] = byte(i % 256)
	}

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	
	part, err := writer.CreateFormFile("firmware", "large.swu")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	
	if _, err := part.Write(largeFileContent); err != nil {
		t.Fatalf("Failed to write file data: %v", err)
	}
	
	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Create request
	req := httptest.NewRequest("POST", "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Create response recorder
	rr := httptest.NewRecorder()

	// Serve request
	handler.ServeHTTP(rr, req)

	// Should get 413 Payload Too Large
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status %d, got %d: %s", http.StatusRequestEntityTooLarge, rr.Code, rr.Body.String())
	}
}
