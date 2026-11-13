# State Machine Package

This package implements the state machine for the Speakerctld Speaker system. It manages the network state transitions according to the system requirements.

## Overview

The state machine manages the following states:
- **FIRSTBOOT_AP**: Initial state, open AP for Wi-Fi provisioning
- **WIFI_CONNECT**: Attempting to connect to configured Wi-Fi
- **ONLINE**: Successfully connected to Wi-Fi
- **FALLBACK_AP**: Fallback state, secured AP for recovery

### State Transitions

```
FIRSTBOOT_AP -> WIFI_CONNECT (on provisioning)
WIFI_CONNECT -> ONLINE (on successful connection)
WIFI_CONNECT -> FALLBACK_AP (on connection failure)
ONLINE -> FALLBACK_AP (on connection loss)
FALLBACK_AP -> WIFI_CONNECT (on retry)
```

## Usage

### Basic State Machine

```go
package main

import (
    "github.com/dchauviere/speakerctld/internal/state"
    "github.com/dchauviere/speakerctld/internal/storage"
)

func main() {
    // Initialize storage
    st := storage.DefaultFileStorage()
    
    // Create state machine
    sm, err := state.NewStateMachine(st)
    if err != nil {
        panic(err)
    }
    
    // Get current state
    current := sm.CurrentState()
    
    // Transition to new state
    err = sm.Transition(state.WifiConnect)
    if err != nil {
        // Handle error
    }
}
```

### Using the Orchestrator

The orchestrator coordinates state transitions with systemd targets and configuration generation:

```go
package main

import (
    "github.com/dchauviere/speakerctld/internal/state"
    "github.com/dchauviere/speakerctld/internal/storage"
)

func main() {
    // Initialize storage
    st := storage.DefaultFileStorage()
    
    // Create orchestrator
    orch, err := state.NewOrchestrator(st, "ABCD")
    if err != nil {
        panic(err)
    }
    defer orch.Close()
    
    // Transition to new state
    // This will:
    // 1. Validate and execute the state transition
    // 2. Generate appropriate configuration files
    // 3. Start/stop systemd targets
    err = orch.TransitionTo(state.WifiConnect)
    if err != nil {
        // Handle error
    }
}
```

### Handling Network Events

Use the `StateTransitionHandler` to handle network events:

```go
package main

import (
    "github.com/dchauviere/speakerctld/internal/state"
    "github.com/dchauviere/speakerctld/internal/storage"
)

func main() {
    st := storage.DefaultFileStorage()
    sm, _ := state.NewStateMachine(st)
    
    handler := state.NewStateTransitionHandler(sm, st)
    
    // Handle Wi-Fi provisioning
    err := handler.HandleProvisioning(&state.WifiProvisionData{
        SSID: "MyNetwork",
        PSK:  "MyPassword",
    })
    
    // Handle Wi-Fi connected
    err = handler.HandleWifiConnected(&state.NetworkEvent{
        Connected: true,
        SSID:      "MyNetwork",
    })
    
    // Handle Wi-Fi disconnected
    err = handler.HandleWifiDisconnected(&state.NetworkEvent{
        Connected: false,
        Error:     errors.New("connection lost"),
    })
}
```

## Files

- `state.go`: Defines the State type and StateMachine interface
- `machine.go`: Implements the state machine with validation and persistence
- `transitions.go`: Handles state transitions based on external events
- `orchestrator.go`: Coordinates state transitions with systemd and configuration
- `machine_test.go`: Unit tests for the state machine

## Error Handling

The state machine returns the following errors:
- `ErrInvalidTransition`: Attempted an invalid state transition
- `ErrNotProvisioned`: Tried to leave FIRSTBOOT_AP without provisioning

## Persistence

The state machine automatically persists the current state and Wi-Fi failure count using the storage backend. All state changes are atomic - if persistence fails, the state is rolled back.

## Wi-Fi Failure Tracking

The state machine tracks Wi-Fi connection failures. After 3 consecutive failures, it automatically transitions to FALLBACK_AP state. The failure count is reset when a new connection attempt is made.
