package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dchauviere/speakerctld/internal/ui"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigDefaultsAndMissingFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	cfg, err := loadConfigPath(nil, io.Discard, missing)
	if err != nil {
		t.Fatal(err)
	}
	want := config{
		wifiInterface: DefaultWifiInterface,
		port:          DefaultPort,
		dataDir:       DefaultDataDir,
		name:          ui.DefaultName,
		description:   ui.DefaultDescription,
		color:         ui.DefaultColor,
		resetButton:   "/dev/input/event0",
		resetHold:     10 * time.Second,
	}
	if cfg != want {
		t.Fatalf("defaults: got %+v, want %+v", cfg, want)
	}
	if _, err := loadConfigPath([]string{"-config", missing}, io.Discard, missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit missing file: got %v", err)
	}
	if _, err := loadConfigPath(nil, io.Discard, t.TempDir()); err == nil {
		t.Fatal("expected an error reading a directory")
	}
}

func TestLoadConfigFileAndCLIOverrides(t *testing.T) {
	path := writeConfig(t, `interface: wifi1
port: 80
data-dir: /somewhere
name: Kitchen
description: "My speaker"
color: green
factory-reset-input: /dev/input/event2
factory-reset-hold: 1m30s
`)
	cfg, err := loadConfigPath([]string{"-config", path}, io.Discard, "unused")
	if err != nil {
		t.Fatal(err)
	}
	want := config{"wifi1", "80", "/somewhere", "Kitchen", "My speaker", "green", "/dev/input/event2", 90 * time.Second}
	if cfg != want {
		t.Fatalf("file: got %+v, want %+v", cfg, want)
	}

	args := []string{
		"-config", path, "-interface", DefaultWifiInterface, "-port", DefaultPort,
		"-data-dir", DefaultDataDir, "-name", "", "-description", "",
		"-color", ui.DefaultColor, "-factory-reset-input", "", "-factory-reset-hold", "10s",
	}
	cfg, err = loadConfigPath(args, io.Discard, "unused")
	if err != nil {
		t.Fatal(err)
	}
	want = config{DefaultWifiInterface, DefaultPort, DefaultDataDir, "", "", ui.DefaultColor, "", 10 * time.Second}
	if cfg != want {
		t.Fatalf("CLI overrides: got %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigPartialAndEmptyValues(t *testing.T) {
	path := writeConfig(t, "name: \"\"\nfactory-reset-input: \"\"\nfactory-reset-hold: 0s\n")
	cfg, err := loadConfigPath(nil, io.Discard, path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.name != "" || cfg.resetButton != "" || cfg.resetHold != 0 || cfg.port != DefaultPort {
		t.Fatalf("partial config: got %+v", cfg)
	}
}

func TestLoadShippedSystemdConfig(t *testing.T) {
	path := filepath.Join("..", "..", "etc", "config.yaml")
	cfg, err := loadConfigPath(nil, io.Discard, path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.port != "80" || cfg.wifiInterface != DefaultWifiInterface || cfg.dataDir != DefaultDataDir ||
		cfg.resetButton != "/dev/input/event0" || cfg.resetHold != 10*time.Second {
		t.Fatalf("systemd config: got %+v", cfg)
	}
	cfg, err = loadConfigPath([]string{"-port", "8080"}, io.Discard, path)
	if err != nil || cfg.port != "8080" {
		t.Fatalf("systemd config CLI override: got %+v, %v", cfg, err)
	}
}

func TestLoadConfigRejectsInvalidFiles(t *testing.T) {
	tests := []struct {
		name, contents, expected string
	}{
		{"empty", "", "decode YAML"},
		{"invalid yaml", "port: [\n", "decode YAML"},
		{"not mapping", "- port\n", "mapping"},
		{"unknown key", "ports: 80\n", "unknown configuration key"},
		{"config key", "config: other.yaml\n", "unknown configuration key"},
		{"duplicate key", "port: 80\nport: 81\n", "duplicate"},
		{"null", "name: null\n", "must be a string"},
		{"wrong type", "name: [one, two]\n", "must be a string"},
		{"boolean", "name: true\n", "must be a string"},
		{"invalid duration", "factory-reset-hold: nope\n", "invalid configuration value"},
		{"multiple documents", "port: 80\n---\nport: 81\n", "only one YAML document"},
		{"bad color", "color: invalid-color\n", "invalid color"},
		{"nonpositive hold", "factory-reset-hold: 0s\n", "must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.contents)
			_, err := loadConfigPath(nil, io.Discard, path)
			if err == nil || !strings.Contains(err.Error(), tt.expected) {
				t.Fatalf("got %v, want error containing %q", err, tt.expected)
			}
		})
	}
}

func TestLoadConfigValidationAndHelp(t *testing.T) {
	path := writeConfig(t, "color: invalid-color\nfactory-reset-hold: -1s\n")
	cfg, err := loadConfigPath([]string{"-color", "blue", "-factory-reset-input", ""}, io.Discard, path)
	if err != nil || cfg.color != "blue" || cfg.resetHold != -time.Second {
		t.Fatalf("effective validation: got %+v, %v", cfg, err)
	}
	path = writeConfig(t, "factory-reset-hold: invalid\n")
	if _, err := loadConfigPath([]string{"-factory-reset-hold", "10s"}, io.Discard, path); err == nil {
		t.Fatal("invalid duration must fail even when overridden")
	}
	var output bytes.Buffer
	if _, err := loadConfigPath([]string{"-help"}, &output, path); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: got %v", err)
	}
	if !strings.Contains(output.String(), "-config") {
		t.Fatal("help does not mention -config")
	}
}
