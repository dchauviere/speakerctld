# State Machine Package

This package implements the state machine for the Speakerctld Speaker system. It manages the network state transitions according to the system requirements.

## Overview

The state machine manages the following states:
- **FIRSTBOOT_AP**: Open captive portal for initial provisioning and Wi-Fi recovery
- **WIFI_CONNECT**: Attempting to connect to configured Wi-Fi
- **ONLINE**: Successfully connected to Wi-Fi

### State Transitions

```
FIRSTBOOT_AP -> WIFI_CONNECT (on Wi-Fi submission)
WIFI_CONNECT -> ONLINE (on successful connection)
WIFI_CONNECT -> FIRSTBOOT_AP (after repeated connection failures or missing credentials)
ONLINE -> WIFI_CONNECT (on connection loss)
ONLINE -> FIRSTBOOT_AP (manual recovery)
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

The orchestrator coordinates state transitions with NetworkManager, systemd targets, and configuration generation:

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
    orch, err := state.NewOrchestrator(st, "ABCD", "wlan0")
    if err != nil {
        panic(err)
    }
    defer orch.Close()
    
    // Transition to new state
    // This will:
    // 1. Validate and execute the state transition
    // 2. Configure NetworkManager and generate the dnsmasq configuration
    // 3. Start/stop systemd targets
    err = orch.TransitionTo(state.WifiConnect)
    if err != nil {
        // Handle error
    }
}
```

### Access point setup

NetworkManager must manage the selected Wi-Fi interface and support AP mode.
It activates an ephemeral open AP profile named `Speakerctld-Setup-<device-id>`
and assigns `192.168.42.1/24`. The same open network is used for recovery;
anyone in range can access the portal while it is active.
Wi-Fi passwords are sent over this open portal without transport encryption;
use the portal only in a trusted environment. Stored Wi-Fi credentials are
written with owner-only permissions.

On first boot the portal shows only the Wi-Fi selection page. Once provisioned,
it shows settings, with a link to select Wi-Fi again. After connection failures,
the portal remains active until Wi-Fi credentials are submitted again. Devices
with a persisted legacy `FALLBACK_AP` state are migrated to `FIRSTBOOT_AP`.

The Wi-Fi page requests an explicit NetworkManager scan on the selected
interface. Scanning while the AP is active depends on the radio and driver and
may briefly interrupt portal clients; errors are displayed and the network can
be entered manually. Open and WPA/WPA2 personal networks are supported;
enterprise, WEP, WPA3-only and OWE networks are shown as unsupported. Hidden
networks can be entered manually.

After NetworkManager reports the AP active, `speakerctld-ap.target` starts
`speakerctld-dnsmasq.service` for DHCP and captive DNS. Install the updated
systemd units, including `wifi-client.target`, and the dnsmasq template along
with the daemon. The former speakerctld hostapd service is no longer used.
The `-interface` option selects the interface for both the AP and dnsmasq.
AP activation errors are logged and cause startup to fail, allowing systemd
to retry. Check the speakerctld and dnsmasq journals if the AP does not start.

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

The state machine tracks Wi-Fi connection failures. After 3 consecutive failures, it returns to the captive portal. Submitting Wi-Fi credentials resets the failure count and starts a new connection attempt.
