# Building Speakerctld with SWUpdate Integration

## Overview

This document describes how to build the Speakerctld application with SWUpdate integration using CGO. It covers development builds, cross-compilation, and Yocto integration.

## Prerequisites

### General Requirements

- Git
- Go 1.21+
- GCC or compatible C compiler
- pkg-config (for CGO)
- Make

### For Cross-Compilation (ARMv7)

- ARM cross-compiler: `gcc-arm-linux-gnueabihf`
- SWUpdate development files for ARM

### For Yocto Build

- Yocto Project (Wrynose 5.2)
- SWUpdate layer in Yocto

## Quick Start

### Clone the Repository

```bash
git clone https://github.com/dchauviere/speakerctld.git
cd speakerctld
```

### Development Build (Without CGO)

For development without SWUpdate integration:

```bash
# Download dependencies
go mod download

# Build
make build

# Or manually
go build -o bin/speakerctld ./cmd/speakerctld
```

### Development Build (With CGO)

For development with SWUpdate integration on the build machine:

```bash
# Install SWUpdate development files
# On Ubuntu/Debian:
sudo apt-get install swupdate-dev

# On Yocto target (from devshell):
# The headers and libraries are already available

# Build with CGO
export CGO_ENABLED=1
export CGO_CFLAGS="-Iinternal/swupdate/c_wrapper"
export CGO_LDFLAGS="-lswupdate"

go build -o bin/speakerctld ./cmd/speakerctld

# Or use make
make CGO_ENABLED=1 build
```

## Cross-Compilation for ARMv7

### Using GCC Cross-Compiler

```bash
# Install cross-compiler
sudo apt-get install gcc-arm-linux-gnueabihf

# Set environment variables
export GOOS=linux
export GOARCH=arm
export GOARM=7
export CGO_ENABLED=1
export CC=arm-linux-gnueabihf-gcc
export CXX=arm-linux-gnueabihf-g++
export CGO_CFLAGS="-Iinternal/swupdate/c_wrapper"
export CGO_LDFLAGS="-L/path/to/swupdate/lib -lswupdate"

# Build
go build -o bin/speakerctld-arm ./cmd/speakerctld

# Or use make
make build-arm
```

### Using Yocto SDK

```bash
# Source the Yocto SDK environment
source /path/to/yocto/sdk/environment-setup-armv7ahf-vfpv4-neon-poky-linux-gnueabi

# Set Go environment
export GOOS=linux
export GOARCH=arm
export GOARM=7
export CGO_ENABLED=1
export CGO_CFLAGS="-Iinternal/swupdate/c_wrapper"
export CGO_LDFLAGS="-L${libdir} -lswupdate"

# Build
go build -o bin/speakerctld ./cmd/speakerctld
```

## Yocto Integration

### Add SWUpdate to Local Configuration

Ensure your Yocto configuration includes SWUpdate:

```bash
# In local.conf
IMAGE_INSTALL:append = " swupdate"
```

### Build with BitBake

```bash
# Clone meta-swupdate if not already present
cd /path/to/yocto
git clone https://github.com/sbabic/meta-swupdate.git

# Add meta-swupdate to bblayers.conf
bitbake-layers add-layer ../meta-swupdate

# Build speakerctld
bitbake speakerctld
```

### Custom Recipe

The provided recipe (`meta-djingo/recipes-connectivity/speakerctld/speakerctld.bb`) handles:

- CGO compilation
- SWUpdate dependency
- Cross-compilation for ARMv7
- Systemd service installation

To customize:

```bash
# Edit the recipe
nano /path/to/yocto/meta-djingo/recipes-connectivity/speakerctld/speakerctld.bb

# Modify CGO flags, socket paths, etc.
```

## Build Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CGO_ENABLED` | Enable CGO | `1` (for SWUpdate) |
| `GOOS` | Target OS | `linux` |
| `GOARCH` | Target architecture | `arm` |
| `GOARM` | ARM version | `7` |
| `CC` | C compiler | System default |
| `CGO_CFLAGS` | C compiler flags | `-Iinternal/swupdate/c_wrapper` |
| `CGO_LDFLAGS` | Linker flags | `-lswupdate` |
| `SWUPDATE_SOCKET_CTRL_PATH` | Control socket path | `/tmp/sockinstctrl` |
| `SWUPDATE_SOCKET_PROGRESS_PATH` | Progress socket path | `/tmp/swupdateprog` |

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make all` | Build the project |
| `make build` | Build the project |
| `make debug` | Build with debug info |
| `make test` | Run all tests |
| `make test-coverage` | Run tests with coverage |
| `make test-race` | Run tests with race detector |
| `make lint` | Run linter |
| `make deps` | Download dependencies |
| `make clean` | Clean build artifacts |
| `make install` | Install to /usr/local/bin |
| `make run` | Build and run |
| `make build-arm` | Build for ARMv7 |
| `make build-arm64` | Build for ARM64 |
| `make build-dev` | Build for development |
| `make run-dev` | Build and run for development |

## Testing

### Unit Tests

```bash
# Run all tests
go test -v ./...

# Run with race detector
go test -race -v ./...

# Run with coverage
go test -coverprofile=coverage.out -v ./...
go tool cover -html=coverage.out
```

### Specific Package Tests

```bash
# Test swupdate package only
go test -v ./internal/swupdate/...

# Test API package only
go test -v ./internal/api/...
```

### Integration Tests

For integration tests, you need:
- SWUpdate daemon running
- Valid .swu files
- Proper socket permissions

```bash
# Start SWUpdate daemon (on target)
swupdate -d

# Run integration tests
# Note: These require a running SWUpdate instance
```

## Troubleshooting

### CGO Compilation Errors

**Error**: `exec: "pkg-config": executable file not found`

**Solution**: Install pkg-config

```bash
sudo apt-get install pkg-config
```

**Error**: `cannot find -lswupdate`

**Solution**: Install SWUpdate development files

```bash
# On Ubuntu/Debian
sudo apt-get install swupdate-dev

# On Yocto
# Ensure SWUpdate is in IMAGE_INSTALL and dev packages are available
```

**Error**: Header files not found

**Solution**: Set include path

```bash
export CGO_CFLAGS="-I/path/to/swupdate/include -Iinternal/swupdate/c_wrapper"
```

### Cross-Compilation Errors

**Error**: Incompatible pointer types

**Solution**: Ensure cross-compiler is compatible

```bash
# Verify cross-compiler
arm-linux-gnueabihf-gcc --version

# Ensure it's compatible with your Go version
```

**Error**: Linker errors

**Solution**: Set correct linker

```bash
export CC=arm-linux-gnueabihf-gcc
export CXX=arm-linux-gnueabihf-g++
export LD=arm-linux-gnueabihf-gcc
```

### Yocto Build Errors

**Error**: Recipe not found

**Solution**: Ensure recipe path is correct

```bash
# Check recipe location
ls meta-djingo/recipes-connectivity/speakerctld/

# Verify it's in bblayers.conf
```

**Error**: Missing dependencies

**Solution**: Add required layers

```bash
# Add meta-swupdate to bblayers.conf
bitbake-layers add-layer ../meta-swupdate
```

## Running the Application

### Local Development

```bash
# Build and run
make run-dev

# Or manually
go build -o /tmp/speakerctld ./cmd/speakerctld
/tmp/speakerctld -interface wlan0 -port 8080 -data-dir /tmp/data
```

### On Target Device

```bash
# Copy binary to device
scp bin/speakerctld user@device:/usr/bin/

# Start service
sudo systemctl start speakerctld

# Check status
sudo systemctl status speakerctld

# View logs
journalctl -u speakerctld -f
```

### With Systemd

Install the provided systemd units and dnsmasq template. NetworkManager must
manage the selected Wi-Fi interface and support AP mode.

```bash
# Install units and template
sudo install -m 644 systemd/speakerctld.service systemd/speakerctld-ap.target \
    systemd/speakerctld-dnsmasq.service systemd/wifi-client.target /etc/systemd/system/
sudo install -d /etc/speakerctld
sudo install -m 644 etc/dnsmasq.conf.gotmpl /etc/speakerctld/
sudo systemctl daemon-reload

# Enable service
sudo systemctl enable speakerctld

# Start service
sudo systemctl start speakerctld
```

## Configuration

### Command Line Options

```bash
speakerctld \
    -interface wlan0 \
    -port 8080 \
    -data-dir /data \
    -factory-reset-input /dev/input/event0 \
    -factory-reset-hold 10s
```

### Factory reset

The settings page offers a confirmed factory reset. Holding `KEY_POWER` on the
configured Linux input device for the configured duration triggers the same
operation; releasing early cancels it. Set `-factory-reset-input ""` to disable
the button. The daemon requires read access to that device and uses logind's
`handle-power-key` inhibitor while monitoring it, so short presses do not
perform logind's normal power-key action. Check the event device assignment on
the target; event numbers can change.

Reset permanently deletes **all contents** of `-data-dir`, including Wi-Fi
credentials, device identity and any unrelated data stored there. Nested
mounts are rejected. The directory itself is retained. A durable marker
ensures that interrupted resets resume before normal startup; logind reboots
the device via D-Bus. Errors are logged, and a failed reset is retried on
service restart. After reboot, the device returns to first-boot provisioning.
If a firmware update started through the daemon is running, reset waits for it
to finish before erasing data or requesting a reboot.
The web endpoint has the same unauthenticated access as the existing settings
page; restrict access to the management network.

### Environment Variables

```bash
# Socket paths (optional)
export SWUPDATE_SOCKET_CTRL_PATH=/tmp/sockinstctrl
export SWUPDATE_SOCKET_PROGRESS_PATH=/tmp/swupdateprog

# Go build tags (for debugging)
export GODEBUG=cgocheck=1
```

## File Locations

### Source Files

```
.
├── cmd/
│   └── speakerctld/
│       └── main.go
├── internal/
│   ├── api/
│   │   ├── captive/
│   │   ├── firmware/
│   │   ├── settings/
│   │   ├── websocket/
│   │   └── server.go
│   ├── state/
│   ├── storage/
│   ├── systemd/
│   ├── network/
│   └── swupdate/
│       ├── c_wrapper/
│       │   ├── swupdate_wrapper.h
│       │   └── swupdate_wrapper.c
│       ├── types.go
│       ├── installer_cgo.go
│       ├── installer_nocgo.go
│       └── README.md
├── systemd/
│   └── speakerctld.service
├── docs/
│   └── SWUPDATE_INTEGRATION.md
└── Makefile
```

### Build Artifacts

- Binary: `bin/speakerctld`
- ARM Binary: `bin/speakerctld-arm`
- Coverage: `coverage.out`, `coverage.html`

## CI/CD Integration

### GitHub Actions

Example workflow:

```yaml
name: Build and Test

on: [push, pull_request]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Download dependencies
        run: go mod download
      
      - name: Build without CGO
        run: make build
      
      - name: Run tests
        run: make test

  build-arm:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install cross-compiler
        run: sudo apt-get install gcc-arm-linux-gnueabihf
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Build for ARM
        run: make build-arm
        env:
          CC: arm-linux-gnueabihf-gcc
```

### GitLab CI

Example configuration:

```yaml
stages:
  - build
  - test

build:
  stage: build
  image: golang:1.21
  script:
    - go mod download
    - make build

build:arm:
  stage: build
  image: golang:1.21
  before_script:
    - apt-get update
    - apt-get install -y gcc-arm-linux-gnueabihf
  script:
    - make build-arm
  artifacts:
    paths:
      - bin/speakerctld-arm

test:
  stage: test
  image: golang:1.21
  script:
    - make test
```

## Version Management

### Get Current Version

```bash
# From git
git describe --tags --always

# From binary (if version is embedded)
speakerctld --version
```

### Set Version

```bash
# Set version in main.go or use ldflags
export LDFLAGS="-X main.Version=1.0.0"
go build -ldflags="$LDFLAGS" -o bin/speakerctld ./cmd/speakerctld
```

## Clean Build

```bash
# Remove all build artifacts
make clean

# Remove go modules cache
rm -rf go.mod go.sum

# Remove vendor directory
rm -rf vendor/

# Remove bin directory
rm -rf bin/
```

## Final Notes

1. **CGO Requirement**: SWUpdate integration requires CGO. Without CGO, the application will run but firmware update functionality will return errors.

2. **Socket Permissions**: Ensure the application has permissions to access SWUpdate sockets (typically `/tmp/sockinstctrl` and `/tmp/swupdateprog`).

3. **SWUpdate Daemon**: The SWUpdate daemon must be running on the target device for updates to work.

4. **Resource Limits**: Large firmware updates may require significant memory. Adjust buffer sizes as needed.

5. **Security**: Ensure proper authentication and authorization for firmware update endpoints in production.
