package state

type State string

const (
    FirstBootAP State = "FIRSTBOOT_AP"
    WifiConnect State = "WIFI_CONNECT"
    Online      State = "ONLINE"
    FallbackAP  State = "FALLBACK_AP"
)

/*
	Allowed Transitions
	FIRSTBOOT_AP -> WIFI_CONNECT
	WIFI_CONNECT -> ONLINE
	WIFI_CONNECT -> FALLBACK_AP
	ONLINE -> FALLBACK_AP
	FALLBACK_AP -> WIFI_CONNECT
*/

type StateMachine interface {
    CurrentState() State
    Transition(State) error
    IsProvisioned() bool
    SetProvisioned()
    WifiFailureCount() int
    ResetWifiFailures()
    IncrementWifiFailures() error
    CanTransitionTo(State) bool
}
