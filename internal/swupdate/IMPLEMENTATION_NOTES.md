# Implementation Notes for SWUpdate Integration

## Overview

This document provides detailed implementation notes for the SWUpdate integration in the Speakerctld project. It explains the design decisions, technical details, and considerations for the integration.

## Architecture

### Component Structure

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Application Layer                              │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐ │
│  │  HTTP API       │    │  WebSocket Hub  │    │  Settings API   │ │
│  │  (firmware/)     │    │  (websocket/)   │    │                 │ │
│  └────────┬────────┘    └────────┬────────┘    └─────────────────┘ │
│           │                     │                              │
│           ▼                     ▼                              │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                         swupdate Package                           │ │
│  │  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────┐ │ │
│  │  │  installer     │    │  installer     │    │   types     │ │ │
│  │  │  (cgo)          │    │  (nocgo)        │    │             │ │ │
│  │  └────────┬────────┘    └────────┬────────┘    └─────────────┘ │ │
│  │           │                     │                           │
│  └───────────┼─────────────────────┼───────────────────────────┘ │
│              │                     │                               │
│              ▼                     ▼                               │
│  ┌───────────────────────────────┐    ┌───────────────────────┐   │
│  │      C Wrapper (CG0)         │    │   Stub Implementation │   │
│  │  ┌─────────────────────────┐ │    │  (No CGO available)    │   │
│  │  │  swupdate_wrapper.h    │ │    │                        │   │
│  │  │  swupdate_wrapper.c    │ │    └───────────────────────┘   │
│  │  └─────────────────────────┘ │                              │
│  └───────────────────────────────┘                              │
│                          │                                      │
│                          ▼                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    SWUpdate C Library                       │   │
│  │   libswupdate.so                                           │   │
│  │   - swupdate_prepare_req()                                  │   │
│  │   - swupdate_async_start()                                  │   │
│  │   - ipc_send_data()                                        │   │
│  │   - progress_ipc_connect()                                  │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

## File Structure

```
internal/swupdate/
├── c_wrapper/
│   ├── swupdate_wrapper.h    # C interface definitions
│   └── swupdate_wrapper.c    # C implementation
├── types.go                  # Go type definitions (shared)
├── installer_cgo.go          # CGO implementation
├── installer_nocgo.go        # Stub for when CGO is disabled
├── example_test.go           # Usage examples and tests
├── swupdate_test.go          # Unit tests
├── README.md                 # Package documentation
└── Makefile                  # Package build file
```

## Design Decisions

### 1. Build Tags for CGO

The package uses Go build tags to handle CGO availability:

- **`+build cgo`**: Files that require CGO (`installer_cgo.go`)
- **`+build !cgo`**: Fallback stubs (`installer_nocgo.go`)

This allows the package to compile with or without CGO support.

### 2. Type Separation

All type definitions are in `types.go` without build tags, ensuring they're available regardless of CGO support. This prevents import cycles and ensures consistent types across the codebase.

### 3. C Wrapper Design

The C wrapper (`c_wrapper/`) provides:

- **Simplified Interface**: Hides SWUpdate's internal structures and complexity
- **Memory Safety**: Manages buffers and memory allocation in C
- **Thread Safety**: Uses mutexes for thread-safe operations
- **Error Handling**: Converts SWUpdate errors to simple return codes
- **Progress Monitoring**: Separate connection to progress socket

### 4. Streaming Data Transfer

Data is streamed rather than loaded entirely into memory:

- **Chunked Reading**: Data is read in configurable buffer sizes (default 128KB)
- **Progressive Writing**: Each chunk is sent to SWUpdate immediately
- **Memory Efficient**: Only one buffer is allocated at a time

### 5. Event-Driven Architecture

Progress and status updates are communicated via:

- **Channels**: Go channels for event notification
- **Subscriptions**: Multiple subscribers can receive events
- **Non-blocking**: Event delivery doesn't block update processing

### 6. WebSocket Integration

Real-time updates are broadcast to connected WebSocket clients:

- **Hub Pattern**: Central hub manages all connections
- **Broadcast**: Events are sent to all connected clients
- **JSON Format**: Events are serialized as JSON

## Technical Details

### C Wrapper Implementation

The C wrapper uses SWUpdate's official IPC functions:

```c
// Initialize SWUpdate
int swupdate_wrapper_init(const char *ctrl_socket_path, const char *prog_socket_path)

// Start an async update
int swupdate_wrapper_start_update(
    UpdateSourceType source_type,
    bool dry_run,
    const char *software_set,
    const char *running_mode
)

// Send data to SWUpdate
int swupdate_wrapper_send_data(const char *buf, int size)

// Get current status
UpdateStatus swupdate_wrapper_get_status(void)

// Monitor progress
int swupdate_progress_receive(SwupdateProgress *progress)
```

### Go Implementation

The Go implementation provides:

```go
// Create installer
installer, err := swupdate.NewInstaller()

// Start update
source := swupdate.UpdateSource{
    Type:     swupdate.SourceLocal,
    FilePath: "/path/to/update.swu",
    DryRun:   false,
}
err = installer.StartUpdate(source)

// Subscribe to events
eventChan, unsubscribe := installer.Subscribe()

// Process events
for event := range eventChan {
    // Handle event
}

// Close when done
installer.Close()
```

### HTTP API Integration

Firmware update endpoints:

- `POST /api/firmware/upload` - Upload and install .swu file
- `GET /api/firmware/status` - Get current update status
- `POST /api/firmware/cancel` - Cancel current update (if supported)

### WebSocket Events

Event types broadcast to WebSocket clients:

- `firmware_update` - Update started/completed
- `firmware_progress` - Progress information
- `firmware_status` - Status changes

## Memory Management

### C Side

- Uses static buffers for data transfer
- Allocates memory for file data when needed
- Properly frees all allocated memory

### Go Side

- Uses channels with limited buffer sizes
- Cleans up resources on Close()
- Uses atomic operations for thread-safe flags

## Error Handling

### Common Errors

- `ErrUpdateInProgress`: Update already running
- `ErrNotInitialized`: Installer not initialized
- `ErrCancelNotSupported`: Cancellation not supported
- `ErrNoUpdateFile`: No file or reader provided
- `ErrCGORequired`: CGO required but not enabled

### Error Propagation

Errors are propagated through:
- Return values from functions
- Error field in UpdateEvent
- Error code in UpdateResult

## Performance Considerations

### Buffer Size

- Default: 128KB (131072 bytes)
- Adjustable via UpdateSource.BufferSize
- Larger buffers: Better throughput, more memory
- Smaller buffers: More frequent updates, more overhead

### Progress Updates

- Non-blocking reads from progress socket
- Updates every 100ms
- Only when data is available

### Concurrent Operations

- Only one update at a time (enforced)
- Multiple event subscribers supported
- Thread-safe operations via mutexes

## Build Configuration

### Without CGO (Development)

```bash
go build -v ./...
```

### With CGO (Production)

```bash
CGO_ENABLED=1 \
CGO_CFLAGS="-Iinternal/swupdate/c_wrapper" \
CGO_LDFLAGS="-lswupdate" \
go build -v ./...
```

### Cross-Compilation

```bash
export GOOS=linux
export GOARCH=arm
export GOARM=7
export CGO_ENABLED=1
export CC=arm-linux-gnueabihf-gcc
export CGO_CFLAGS="-Iinternal/swupdate/c_wrapper"
export CGO_LDFLAGS="-L/path/to/swupdate/lib -lswupdate"

go build -o bin/speakerctld ./cmd/speakerctld
```

### Yocto

The Yocto recipe (`speakerctld.bb`) handles:
- CGO compilation
- Cross-compilation for ARMv7
- SWUpdate dependency
- Systemd service installation

## Testing

### Unit Tests

```bash
# Run all tests
go test -v ./internal/swupdate/...

# Run with race detector
go test -race -v ./internal/swupdate/...

# Run with coverage
go test -coverprofile=coverage.out -v ./internal/swupdate/...
go tool cover -html=coverage.out
```

### Integration Tests

Integration tests require:
- SWUpdate daemon running
- Proper socket permissions
- Valid .swu files

## Debugging

### Enable Verbose Logging

```bash
export GODEBUG=cgocheck=1
speakerctld -data-dir /tmp/speakerctld-data
```

### Check SWUpdate Logs

```bash
journalctl -u swupdate -f
```

### Verify Socket Connections

```bash
# Check if control socket exists
ls -la /tmp/sockinstctrl

# Check if progress socket exists
ls -la /tmp/swupdateprog

# Test socket connection
socat - UNIX-CONNECT:/tmp/swupdateprog
```

## Known Limitations

1. **No Cancellation**: SWUpdate IPC doesn't support cancellation
2. **Single Update**: Only one update at a time is supported
3. **ARMv7 Only**: Currently tested on ARMv7 32-bit
4. **SWUpdate Version**: Requires SWUpdate 2026.05.1 or compatible
5. **Memory**: Large updates may consume significant memory during streaming

## Future Enhancements

- [ ] Add support for delta updates
- [ ] Implement chunked uploads for large files
- [ ] Add authentication to WebSocket connections
- [ ] Support for multiple simultaneous updates (if SWUpdate supports it)
- [ ] Add metrics and monitoring endpoints
- [ ] Implement retry logic for failed updates
- [ ] Add support for SWUpdate's download functionality
- [ ] Implement signature verification in Go

## Compatibility

### SWUpdate Versions

- **Tested**: 2026.05.1
- **Expected to work**: 2025.12 and later (with IPC support)
- **Not compatible**: Versions without IPC interface

### Go Versions

- **Minimum**: Go 1.21 (for CGO support)
- **Recommended**: Go 1.21+

### Platforms

- **Primary**: Linux ARMv7
- **Expected to work**: Linux ARM64, x86_64
- **Not supported**: Windows, macOS (due to Unix socket dependency)

## References

- [SWUpdate Documentation](https://sbabic.github.io/swupdate/)
- [SWUpdate GitHub](https://github.com/sbabic/swupdate)
- [CGO Documentation](https://pkg.go.dev/cmd/cgo)
- [Go Build Constraints](https://pkg.go.dev/go/build#hdr-Build_Constraints)
