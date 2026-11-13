# SWUpdate Go Package

This package provides integration with [SWUpdate](https://sbabic.github.io/swupdate/) for software updates in Go applications. It uses CGO to call the official SWUpdate C library functions directly.

## Features

- Direct integration with SWUpdate via IPC (Unix Domain Sockets)
- No external processes required
- No Mongoose web server dependency
- Real-time progress monitoring via Unix socket
- Streaming data transfer (no need to load entire files in memory)
- Thread-safe operations
- Event-based notifications via channels
- WebSocket broadcasting support

## Requirements

- Go 1.21+
- SWUpdate 2026.05.1 (headers and library)
- CGO enabled
- GCC or compatible compiler

## Installation

```bash
# Ensure SWUpdate is installed with development files
# On Yocto: bitbake swupdate -c devshell

# Build with CGO
export CGO_ENABLED=1
export CGO_CFLAGS="-Iinternal/swupdate/c_wrapper"
export CGO_LDFLAGS="-lswupdate"

go get github.com/david/speakerctld/internal/swupdate
```

## Usage

### Basic Example

```go
package main

import (
    "fmt"
    "log"
    "github.com/david/speakerctld/internal/swupdate"
)

func main() {
    // Create installer
    installer, err := swupdate.NewInstaller()
    if err != nil {
        log.Fatalf("Failed to create installer: %v", err)
    }
    defer installer.Close()

    // Subscribe to events
    eventChan, unsubscribe := installer.Subscribe()
    defer unsubscribe()

    // Start update from file
    source := swupdate.UpdateSource{
        Type:     swupdate.SourceLocal,
        FilePath: "/path/to/update.swu",
        DryRun:   false,
    }

    if err := installer.StartUpdate(source); err != nil {
        log.Fatalf("Failed to start update: %v", err)
    }

    // Process events
    for event := range eventChan {
        fmt.Printf("Event: %s, Status: %s\n", event.Type, event.Status.String())
        
        if event.Type == "failed" {
            fmt.Printf("Error: %v\n", event.Error)
            break
        }
        
        if event.Progress != nil {
            fmt.Printf("Progress: %d%% (Step %d/%d)\n",
                event.Progress.DownloadPercent,
                event.Progress.CurrentStep,
                event.Progress.TotalSteps)
        }
        
        if event.Type == "completed" {
            fmt.Println("Update completed successfully!")
            break
        }
    }
}
```

### With Custom Socket Paths

```go
// Create installer with custom socket paths
installer, err := swupdate.NewInstallerWithPaths(
    "/tmp/custom-ctrl-socket",
    "/tmp/custom-prog-socket",
)
```

### Streaming from io.Reader

```go
file, err := os.Open("/path/to/update.swu")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

source := swupdate.UpdateSource{
    Type:   swupdate.SourceLocal,
    Reader: file,
    FileSize: 1048576, // 1MB (optional, for progress tracking)
}

if err := installer.StartUpdate(source); err != nil {
    log.Fatal(err)
}
```

### With Software Set and Running Mode

```go
source := swupdate.UpdateSource{
    Type:         swupdate.SourceLocal,
    FilePath:     "/path/to/update.swu",
    SoftwareSet:  "stable",
    RunningMode:  "production",
    DryRun:       false,
}
```

### Using Custom Buffer Size

```go
source := swupdate.UpdateSource{
    Type:       swupdate.SourceLocal,
    FilePath:   "/path/to/update.swu",
    BufferSize: 262144, // 256KB
}
```

## API Reference

### Types

#### UpdateStatus

Represents the current status of an update:

```go
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
```

#### Progress

Represents the current progress of an update:

```go
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
```

#### UpdateEvent

Represents an event during the update process:

```go
type UpdateEvent struct {
    Type     string    `json:"type"`
    Status   UpdateStatus `json:"status"`
    Progress *Progress `json:"progress,omitempty"`
    Error    error    `json:"error,omitempty"`
    Message  string   `json:"message,omitempty"`
}
```

#### UpdateSource

Defines the source for an update:

```go
type UpdateSource struct {
    Type         int    // Source type constant
    FilePath     string // Path to .swu file (for local source)
    Reader       io.Reader // Reader for streaming data
    FileSize     int64  // Total size of the update file
    SoftwareSet  string // Software set name (optional)
    RunningMode  string // Running mode (optional)
    DryRun       bool   // If true, only simulate the update
    BufferSize   int    // Buffer size for data transfer
}
```

#### UpdateResult

Represents the result of an update operation:

```go
type UpdateResult struct {
    Success bool   `json:"success"`
    Code    int    `json:"code"`
    Message string `json:"message"`
}
```

### Installer Interface

```go
type Installer interface {
    // StartUpdate starts a new update with the given source
    StartUpdate(source UpdateSource) error
    
    // CancelUpdate attempts to cancel the current update
    // Note: SWUpdate may not support cancellation
    CancelUpdate() error
    
    // GetStatus returns the current update status
    GetStatus() UpdateStatus
    
    // IsRunning returns whether an update is currently running
    IsRunning() bool
    
    // GetProgress returns the current progress information
    GetProgress() Progress
    
    // Subscribe returns a channel for receiving update events
    // The returned function can be called to unsubscribe
    Subscribe() (<-chan UpdateEvent, func())
    
    // WaitForCompletion waits for the update to complete
    WaitForCompletion(timeout time.Duration) UpdateResult
    
    // Close shuts down the client and releases resources
    Close() error
}
```

### Source Type Constants

```go
const (
    SourceUnknown          = iota
    SourceWebserver        
    SourceSuricatta        
    SourceDownloader       
    SourceLocal            
    SourceChunksDownloader 
)
```

## Error Handling

Common errors:

- `ErrUpdateInProgress`: An update is already running
- `ErrNotInitialized`: The installer is not initialized
- `ErrCancelNotSupported`: Cancellation is not supported
- `ErrNoUpdateFile`: No update file or reader provided

## Thread Safety

All methods are thread-safe and can be called from multiple goroutines:
- Uses mutexes for internal state protection
- Atomic operations for running flag
- Channel-based event notifications

## Performance

- **Memory**: Streaming design, only buffers one chunk at a time
- **CPU**: Minimal overhead, most work done by SWUpdate
- **Concurrency**: Only one update at a time (enforced)
- **Progress Updates**: Non-blocking, via separate socket connection

## Examples

See `example_test.go` for more usage examples.

## Testing

```bash
# Run all tests
go test -v ./internal/swupdate/...

# Run with race detector
go test -race -v ./internal/swupdate/...

# Run with coverage
go test -coverprofile=coverage.out -v ./internal/swupdate/...
go tool cover -html=coverage.out
```

## Implementation Details

### C Wrapper

The C wrapper (`c_wrapper/`) provides:

1. **Simplified Interface**: Hides SWUpdate's internal structures
2. **Safe Callbacks**: Manages C callbacks internally
3. **Memory Management**: Handles buffer allocation/deallocation
4. **Error Handling**: Converts SWUpdate errors to simple return codes

### Progress Monitoring

Progress is monitored via:
1. **Unix Domain Socket**: `/tmp/swupdateprog` (default)
2. **Non-blocking Reads**: Periodic checks for new messages
3. **Structured Data**: Progress messages decoded into Go structs

### Data Transfer

Data is transferred via:
1. **Chunked Reading**: Reads data in configurable buffer sizes
2. **Streaming**: No need to load entire file in memory
3. **Efficient**: Direct CGO calls to SWUpdate's write functions

## Dependencies

- [SWUpdate](https://sbabic.github.io/swupdate/) 2026.05.1+
- [Go](https://golang.org/) 1.21+

## License

MIT License - see LICENSE file for details.
