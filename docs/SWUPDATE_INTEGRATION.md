# SWUpdate Integration Guide

## Overview

This document describes the integration of SWUpdate into the Speakerctld application using CGO. The integration allows the Go application to directly control SWUpdate installations and monitor progress without using the Mongoose web server.

## Architecture

The integration consists of several layers:

```
┌─────────────────────────────────────────────────────────────┐
│                     Speakerctld Go Application                 │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────┐    ┌─────────────────┐    ┌───────────┐ │
│  │   HTTP API      │    │  WebSocket Hub  │    │  Settings  │ │
│  │  (firmware/*)   │    │  (ws://)        │    │   API      │ │
│  └────────┬────────┘    └────────┬────────┘    └───────┬───┘ │
│           │                     │                   │         │
│           ▼                     ▼                   ▼         │
│  ┌───────────────────────────────────────────────────────┐  │
│  │                   swupdate Package                     │  │
│  │              (internal/swupdate/)                      │  │
│  │  ┌─────────────────┐    ┌───────────────────────────┐ │  │
│  │  │  Installer      │    │    Progress Monitor       │ │  │
│  │  │  Interface       │    │    (via Unix Socket)      │ │  │
│  │  └────────┬────────┘    └─────────────┬─────────────┘ │  │
│  │           │                              │              │  │
│  │           ▼                              ▼              │  │
│  │  ┌─────────────────────────────────────────────────┐   │  │
│  │  │               CGO Binding                        │   │  │
│  │  │  (swupdate_wrapper.h / swupdate_wrapper.c)      │   │  │
│  │  └─────────────────┬───────────────────────────────┘   │  │
│  │                    │                                  │  │
│  └────────────────────▼──────────────────────────────────┘  │
│                           │                                  │
│                           ▼                                  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │            SWUpdate C Library (libswupdate)            │  │
│  │  ┌─────────────────┐    ┌───────────────────────────┐ │  │
│  │  │  IPC Interface  │    │    Progress Socket         │ │  │
│  │  │  (Control)       │    │    (/tmp/swupdateprog)     │ │  │
│  │  │  /tmp/sockinstctrl│    │                            │ │  │
│  │  └─────────────────┘    └───────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Components

### 1. C Wrapper (`internal/swupdate/c_wrapper/`)

The C wrapper provides a simplified interface between Go and SWUpdate's C API. It:

- Uses official SWUpdate functions: `swupdate_prepare_req()`, `swupdate_async_start()`, etc.
- Manages C callbacks internally
- Handles memory management safely
- Provides a stable ABI for CGO

**Key Files:**
- `swupdate_wrapper.h`: C interface definitions
- `swupdate_wrapper.c`: Implementation using SWUpdate IPC

### 2. Go Package (`internal/swupdate/`)

The Go package provides a high-level interface for SWUpdate operations:

- `Installer` interface: Defines update management methods
- `Progress` struct: Represents current update progress
- `UpdateEvent` struct: Represents events during update
- `UpdateSource` struct: Defines update source configuration

**Key Features:**
- Thread-safe operations
- Event subscription for progress updates
- Streaming data transfer without loading entire files into memory
- Proper resource cleanup

### 3. HTTP API (`internal/api/firmware/`)

Provides REST endpoints for firmware updates:

- `POST /api/firmware/upload`: Upload and install .swu file
- `GET /api/firmware/status`: Get current update status
- `POST /api/firmware/cancel`: Cancel current update (if supported)

**Query Parameters:**
- `dryrun=true`: Perform dry run only
- `software_set=<name>`: Software set name
- `running_mode=<mode>`: Running mode

### 4. WebSocket Integration (`internal/api/websocket/`)

Real-time event broadcasting to connected clients:

- Progress updates
- Status changes
- Completion/failure notifications

**Event Types:**
- `firmware_update`: Update started/completed
- `firmware_progress`: Progress information
- `firmware_status`: Status changes

## Usage Examples

### Basic Usage

```go
// Create installer
installer, err := swupdate.NewInstaller()
if err != nil {
    log.Fatal(err)
}
defer installer.Close()

// Subscribe to events
endventChan, unsubscribe := installer.Subscribe()
defer unsubscribe()

// Start update from file
source := swupdate.UpdateSource{
    Type:     swupdate.SourceLocal,
    FilePath: "/path/to/update.swu",
    DryRun:   false,
}

if err := installer.StartUpdate(source); err != nil {
    log.Fatal(err)
}

// Process events
for event := range eventChan {
    fmt.Printf("Event: %s, Status: %s\n", event.Type, event.Status)
    if event.Progress != nil {
        fmt.Printf("  Progress: %d%%\n", event.Progress.DownloadPercent)
    }
}
```

### With Reader (HTTP Upload)

```go
// In HTTP handler
tempFile, err := os.CreateTemp("/tmp", "upload-*.swu")
if err != nil {
    http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
    return
}
defer os.Remove(tempFile.Name())

// Copy uploaded file
if _, err := io.Copy(tempFile, r.Body); err != nil {
    http.Error(w, "Failed to save file", http.StatusInternalServerError)
    return
}

// Seek to beginning
tempFile.Seek(0, 0)

// Start update
source := swupdate.UpdateSource{
    Type:   swupdate.SourceLocal,
    Reader: tempFile,
}

if err := installer.StartUpdate(source); err != nil {
    http.Error(w, "Failed to start update", http.StatusInternalServerError)
    return
}
```

## Compilation

### Requirements

- Go 1.21+
- GCC cross-compiler for ARM
- SWUpdate 2026.05.1 development files
- pkg-config

### Development Build (Local)

```bash
# Install dependencies
sudo apt-get install gcc-arm-linux-gnueabihf pkg-config

# Ensure SWUpdate headers and libraries are installed
# On Yocto, use: bitbake swupdate -c devshell

# Build with CGO
export CGO_ENABLED=1
export CGO_CFLAGS="-Iinternal/swupdate/c_wrapper"
export CGO_LDFLAGS="-lswupdate"
go build -o bin/speakerctld ./cmd/speakerctld
```

### Cross-Compilation (ARMv7)

```bash
export GOOS=linux
export GOARCH=arm
export GOARM=7
export CGO_ENABLED=1
export CC=arm-linux-gnueabihf-gcc
export CXX=arm-linux-gnueabihf-g++
export CGO_CFLAGS="-Iinternal/swupdate/c_wrapper"
export CGO_LDFLAGS="-L/path/to/swupdate/lib -lswupdate"

go build -o bin/speakerctld-arm ./cmd/speakerctld
```

### Yocto Build

The recipe `speakerctld.bb` handles:
- CGO compilation
- SWUpdate dependency
- Cross-compilation for ARMv7
- Systemd service installation

Build with:
```bash
bitbake speakerctld
```

## Configuration

### Environment Variables

- `SWUPDATE_SOCKET_CTRL_PATH`: Path to SWUpdate control socket (default: `/tmp/sockinstctrl`)
- `SWUPDATE_SOCKET_PROGRESS_PATH`: Path to SWUpdate progress socket (default: `/tmp/swupdateprog`)

### Command Line Options

```bash
speakerctld \
    -interface wlan0 \
    -captive-port 8080 \
    -settings-port 8081 \
    -data-dir /data
```

## API Reference

### Types

#### UpdateSource

```go
type UpdateSource struct {
    Type         int    // Source type (SourceLocal, SourceWebserver, etc.)
    FilePath     string // Path to .swu file
    Reader       io.Reader // Reader for streaming data
    FileSize     int64  // Total size of the update file
    SoftwareSet  string // Software set name
    RunningMode  string // Running mode
    DryRun       bool   // If true, only simulate the update
    BufferSize   int    // Buffer size for data transfer
}
```

#### Progress

```go
type Progress struct {
    Status         UpdateStatus
    DownloadPercent int
    DownloadBytes  int64
    TotalSteps     int
    CurrentStep    int
    StepPercent    int
    CurrentImage   string
    HandlerName    string
    Message        string
    Source         int
}
```

#### UpdateStatus

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

### Installer Interface

```go
type Installer interface {
    StartUpdate(source UpdateSource) error
    CancelUpdate() error
    GetStatus() UpdateStatus
    IsRunning() bool
    GetProgress() Progress
    Subscribe() (<-chan UpdateEvent, func())
    WaitForCompletion(timeout time.Duration) UpdateResult
    Close() error
}
```

## WebSocket Events

### Connection

Connect to: `ws://<device-ip>:<settings-port>/ws`

### Event Types

#### firmware_update

Sent when update starts or completes:

```json
{
  "type": "firmware_update",
  "payload": {
    "status": "started",
    "message": "Update started"
  }
}

{
  "type": "firmware_update",
  "payload": {
    "success": true,
    "message": "Update completed successfully",
    "duration": "2m30s"
  }
}
```

#### firmware_progress

Sent during update with progress information:

```json
{
  "type": "firmware_progress",
  "payload": {
    "status": "run",
    "download_percent": 45,
    "download_bytes": 1048576,
    "total_steps": 5,
    "current_step": 2,
    "step_percent": 70,
    "current_image": "rootfs",
    "handler_name": "raw",
    "message": "Installing image",
    "source": "local"
  }
}
```

#### firmware_status

Sent when status changes:

```json
{
  "type": "firmware_status",
  "payload": {
    "status": "run",
    "message": "Installation in progress"
  }
}
```

## Security Considerations

1. **Authentication**: Ensure HTTP endpoints require authentication
2. **File Validation**: Validate .swu files before processing
3. **Size Limits**: Enforce maximum file size limits
4. **Rate Limiting**: Implement rate limiting for uploads
5. **TLS**: Use HTTPS for production deployments

## Troubleshooting

### Common Issues

1. **CGO Compilation Errors**:
   - Ensure SWUpdate headers are in include path
   - Verify libswupdate.so is available
   - Check cross-compiler is properly configured

2. **Socket Connection Errors**:
   - Verify SWUpdate daemon is running
   - Check socket paths exist
   - Verify permissions on socket files

3. **Memory Issues**:
   - Reduce buffer size if needed
   - Monitor memory usage during large updates

### Debugging

Enable verbose logging:
```bash
export GODEBUG=cgocheck=1
speakerctld -data-dir /tmp/speakerctld-data
```

Check SWUpdate logs:
```bash
journalctl -u swupdate -f
```

## Performance

- **Buffer Size**: Default 128KB, adjustable
- **Memory Usage**: Streaming design minimizes memory footprint
- **Concurrent Updates**: Only one update at a time (enforced)
- **Progress Updates**: Non-blocking, via separate socket

## Limitations

1. **Cancel Not Supported**: SWUpdate IPC doesn't support cancellation
2. **Single Update**: Only one update can run at a time
3. **ARMv7 Only**: Currently tested on ARMv7 32-bit
4. **SWUpdate Version**: Requires SWUpdate 2026.05.1 or compatible

## Future Enhancements

- [ ] Add support for delta updates
- [ ] Implement chunked uploads
- [ ] Add authentication to WebSocket
- [ ] Support for multiple simultaneous updates (if SWUpdate supports it)
- [ ] Add metrics and monitoring
- [ ] Implement retry logic for failed updates
