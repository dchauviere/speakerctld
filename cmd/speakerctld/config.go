package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/dchauviere/speakerctld/internal/ui"
	"gopkg.in/yaml.v3"
)

const defaultConfigPath = "/etc/speakerctld/config.yaml"

type config struct {
	wifiInterface string
	port          string
	dataDir       string
	name          string
	description   string
	color         string
	resetButton   string
	resetHold     time.Duration
}

func configFlags(cfg *config, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet("speakerctld", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.wifiInterface, "interface", DefaultWifiInterface, "WiFi interface to monitor")
	flags.StringVar(&cfg.port, "port", DefaultPort, "HTTP port for web server")
	flags.StringVar(&cfg.dataDir, "data-dir", DefaultDataDir, "Data directory")
	flags.StringVar(&cfg.name, "name", ui.DefaultName, "Speaker name displayed in the web interface")
	flags.StringVar(&cfg.description, "description", ui.DefaultDescription, "Description displayed in the web interface")
	flags.StringVar(&cfg.color, "color", ui.DefaultColor, "Dominant color of the web interface (name or hex, e.g. green or #2a9d62)")
	flags.StringVar(&cfg.resetButton, "factory-reset-input", "/dev/input/event0", "Linux input device for factory reset (empty to disable)")
	flags.DurationVar(&cfg.resetHold, "factory-reset-hold", 10*time.Second, "Duration to hold KEY_POWER for factory reset")
	return flags
}

func loadConfig(args []string, output io.Writer) (config, error) {
	return loadConfigPath(args, output, defaultConfigPath)
}

func loadConfigPath(args []string, output io.Writer, defaultPath string) (config, error) {
	var cfg config
	flags := configFlags(&cfg, output)
	path := flags.String("config", defaultPath, "YAML configuration file")
	if err := flags.Parse(args); err != nil {
		return cfg, err
	}

	explicit := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	file, err := os.Open(*path)
	if err != nil {
		if !explicit["config"] && errors.Is(err, os.ErrNotExist) {
			return validateConfig(cfg)
		}
		return cfg, fmt.Errorf("open configuration %q: %w", *path, err)
	}
	defer file.Close()

	var fileCfg config
	fileFlags := configFlags(&fileCfg, io.Discard)
	if err := readConfig(file, fileFlags); err != nil {
		return cfg, fmt.Errorf("read configuration %q: %w", *path, err)
	}
	var mergeErr error
	fileFlags.Visit(func(f *flag.Flag) {
		if !explicit[f.Name] && mergeErr == nil {
			mergeErr = flags.Set(f.Name, f.Value.String())
		}
	})
	if mergeErr != nil {
		return cfg, fmt.Errorf("apply configuration %q: %w", *path, mergeErr)
	}
	return validateConfig(cfg)
}

func readConfig(r io.Reader, flags *flag.FlagSet) error {
	decoder := yaml.NewDecoder(r)
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		return fmt.Errorf("decode YAML: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("configuration must be a YAML mapping")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return fmt.Errorf("decode YAML: %w", err)
		}
		return errors.New("configuration must contain only one YAML document")
	}

	seen := make(map[string]bool)
	fields := doc.Content[0].Content
	for i := 0; i < len(fields); i += 2 {
		key, value := fields[i], fields[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return fmt.Errorf("line %d: configuration keys must be strings", key.Line)
		}
		if seen[key.Value] {
			return fmt.Errorf("line %d: duplicate configuration key %q", key.Line, key.Value)
		}
		seen[key.Value] = true
		if flags.Lookup(key.Value) == nil {
			return fmt.Errorf("line %d: unknown configuration key %q", key.Line, key.Value)
		}
		if value.Kind != yaml.ScalarNode || (value.Tag != "!!str" && !(key.Value == "port" && value.Tag == "!!int")) {
			return fmt.Errorf("line %d: configuration value %q must be a string", value.Line, key.Value)
		}
		text := value.Value
		if value.Tag == "!!int" {
			var number int64
			if err := value.Decode(&number); err != nil {
				return fmt.Errorf("line %d: invalid port: %w", value.Line, err)
			}
			text = strconv.FormatInt(number, 10)
		}
		if err := flags.Set(key.Value, text); err != nil {
			return fmt.Errorf("line %d: invalid configuration value %q: %w", value.Line, key.Value, err)
		}
	}
	return nil
}

func validateConfig(cfg config) (config, error) {
	if _, _, _, err := ui.ParseColor(cfg.color); err != nil {
		return cfg, err
	}
	if cfg.resetButton != "" && cfg.resetHold <= 0 {
		return cfg, errors.New("factory-reset-hold must be positive")
	}
	return cfg, nil
}
