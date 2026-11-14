package storage

// WifiConfig contains the Wi-Fi network configuration.
type WifiConfig struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk,omitempty"`
}

// SystemState contains the persistent system state.
type SystemState struct {
	Provisioned      bool   `json:"provisioned"`
	State            string `json:"state"`
	WifiFailureCount int    `json:"wifiFailureCount"`
}

// Storage is the interface for persistent storage operations.
type Storage interface {
	// WifiConfig operations
	LoadWifiConfig() (*WifiConfig, error)
	SaveWifiConfig(*WifiConfig) error

	// SystemState operations
	LoadSystemState() (*SystemState, error)
	SaveSystemState(*SystemState) error
}

// LoadWifiConfig loads the Wi-Fi configuration from storage.
func LoadWifiConfig() (*WifiConfig, error) {
	// Default implementation uses FileStorage
	if config, err := DefaultFileStorage().LoadWifiConfig(); err == nil && config == nil {
		return &WifiConfig{}, nil
	}
	return DefaultFileStorage().LoadWifiConfig()
}

// SaveWifiConfig saves the Wi-Fi configuration to storage.
func SaveWifiConfig(config *WifiConfig) error {
	// Default implementation uses FileStorage
	return DefaultFileStorage().SaveWifiConfig(config)
}

// LoadSystemState loads the system state from storage.
func LoadSystemState() (*SystemState, error) {
	// Default implementation uses FileStorage
	if state, err := DefaultFileStorage().LoadSystemState(); err == nil && state == nil {
		return &SystemState{Provisioned: false, State: "FIRSTBOOT_AP", WifiFailureCount: 0}, nil
	}
	return DefaultFileStorage().LoadSystemState()
}

// SaveSystemState saves the system state to storage.
func SaveSystemState(state *SystemState) error {
	// Default implementation uses FileStorage
	return DefaultFileStorage().SaveSystemState(state)
}
