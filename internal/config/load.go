package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// Load reads, expands, parses, and merges all configuration layers and
// returns the effective Config along with the list of files actually
// read (in merge order; missing layers are omitted).
//
// A missing file is not an error; an unparseable file IS.
func Load() (*Config, []string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("get cwd: %w", err)
	}
	return loadFromDirs(cwd)
}

// loadFromDirs is the testable core: it takes the cwd explicitly so
// tests can simulate arbitrary working directories.
func loadFromDirs(cwd string) (*Config, []string, error) {
	var loaded []string

	globalPath, err := globalConfigPath()
	if err != nil {
		return nil, nil, fmt.Errorf("locate global config: %w", err)
	}

	globalCfg, ok, err := readConfigFile(globalPath)
	if err != nil {
		return nil, nil, err
	}
	if ok {
		loaded = append(loaded, globalPath)
	}

	localPath := localConfigPath(cwd)
	var localCfg *Config
	if localPath != "" {
		var found bool
		localCfg, found, err = readConfigFile(localPath)
		if err != nil {
			return nil, nil, err
		}
		if found {
			loaded = append(loaded, localPath)
		}
	}

	merged := Merge(Defaults(), globalCfg, localCfg)
	return merged, loaded, nil
}

// readConfigFile reads, env-expands, and YAML-decodes one file. The
// "found" return is true only when the file exists and was successfully
// parsed; nil + false + nil signals "treat as missing layer".
func readConfigFile(path string) (*Config, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}

	expanded := Expand(raw)

	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		// goccy/go-yaml errors already carry line/column info in their
		// String form; prepend the path so the user can locate it.
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, true, nil
}
