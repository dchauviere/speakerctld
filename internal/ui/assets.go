package ui

import (
	"embed"
	"io/fs"
)

//go:embed captive/index.html captive/captive.css captive/captive.js
var captiveAssets embed.FS

//go:embed settings/index.html settings/rebooting.html settings/settings.css settings/settings.js
var settingsAssets embed.FS

// CaptiveAssets returns the embedded filesystem for captive portal assets
func CaptiveAssets() fs.FS {
	return captiveAssets
}

// SettingsAssets returns the embedded filesystem for settings assets
func SettingsAssets() fs.FS {
	return settingsAssets
}

// SettingsData represents settings data for the settings page
type SettingsData struct {
	DeviceName  string `json:"deviceName"`
	CurrentSSID string `json:"currentSSID"`
}

// FirmwareInfo represents firmware information
type FirmwareInfo struct {
	Version string `json:"version"`
	Date    string `json:"date"`
}

// SystemInfo represents system information
type SystemInfo struct {
	Mode      string `json:"mode"`
	State     string `json:"state"`
	IPAddress string `json:"ipAddress"`
	SSID      string `json:"ssid"`
}
