package state

type State string

const (
	FirstBootAP State = "FIRSTBOOT_AP"
	WifiConnect State = "WIFI_CONNECT"
	Online      State = "ONLINE"
)

/*
	Allowed Transitions
	FIRSTBOOT_AP -> WIFI_CONNECT
	WIFI_CONNECT -> ONLINE
	WIFI_CONNECT -> FIRSTBOOT_AP
	ONLINE -> WIFI_CONNECT
	ONLINE -> FIRSTBOOT_AP
*/

type StateMachine interface {
	CurrentState() State
	Transition(State) error
	IsProvisioned() bool
	SetProvisioned() error
	WifiFailureCount() int
	ResetWifiFailures() error
	IncrementWifiFailures() error
	CanTransitionTo(State) bool
}
