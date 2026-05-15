package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type cbConfig struct {
	Home      string // ~/.local/share/back2base (extracted assets)
	ConfigDir string // ~/.config/back2base (user state parent)
	StateDir  string // ~/.config/back2base/state (bind-mounted as ~/.claude)
	EnvFile   string // ~/.config/back2base/env
}

func resolveConfig() cbConfig {
	home := os.Getenv("HOME")

	cbHome := os.Getenv("BACK2BASE_HOME")
	if cbHome == "" {
		cbHome = filepath.Join(home, ".local", "share", "back2base")
	}

	cfgDir := os.Getenv("BACK2BASE_CONFIG")
	if cfgDir == "" {
		cfgDir = filepath.Join(home, ".config", "back2base")
	}

	stateDir := filepath.Join(cfgDir, "state")

	return cbConfig{
		Home:      cbHome,
		ConfigDir: cfgDir,
		StateDir:  stateDir,
		EnvFile:   filepath.Join(cfgDir, "env"),
	}
}

func (c cbConfig) ensureDirs() error {
	dirs := []string{
		c.Home,
		c.ConfigDir,
		c.StateDir,
		filepath.Join(c.StateDir, "history"),
		filepath.Join(c.ConfigDir, "datadog-mcp"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

func readEnvFile(path string) map[string]string {
	result := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return result
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return result
}

func setEnvValue(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Strip optional leading "#" and spaces to match commented-out keys.
		// Use TrimPrefix (not TrimLeft) to avoid stripping multiple characters.
		bare := trimmed
		if strings.HasPrefix(bare, "#") {
			bare = strings.TrimSpace(bare[1:])
		}
		if k, _, ok := strings.Cut(bare, "="); ok && strings.TrimSpace(k) == key {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}

	if !found {
		lines = append(lines, key+"="+value)
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
