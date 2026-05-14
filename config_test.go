package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BACK2BASE_HOME", "")
	t.Setenv("BACK2BASE_CONFIG", "")

	cfg := resolveConfig()

	wantHome := filepath.Join(home, ".local", "share", "back2base")
	if cfg.Home != wantHome {
		t.Errorf("Home = %q, want %q", cfg.Home, wantHome)
	}

	wantConfig := filepath.Join(home, ".config", "back2base")
	if cfg.ConfigDir != wantConfig {
		t.Errorf("ConfigDir = %q, want %q", cfg.ConfigDir, wantConfig)
	}

	wantState := filepath.Join(wantConfig, "state")
	if cfg.StateDir != wantState {
		t.Errorf("StateDir = %q, want %q", cfg.StateDir, wantState)
	}
}

func TestOverridePaths(t *testing.T) {
	t.Setenv("BACK2BASE_HOME", "/custom/home")
	t.Setenv("BACK2BASE_CONFIG", "/custom/config")

	cfg := resolveConfig()

	if cfg.Home != "/custom/home" {
		t.Errorf("Home = %q, want /custom/home", cfg.Home)
	}
	if cfg.ConfigDir != "/custom/config" {
		t.Errorf("ConfigDir = %q, want /custom/config", cfg.ConfigDir)
	}
}

func TestReadEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	content := `# comment
CLAUDE_CODE_OAUTH_TOKEN=tok123
ANTHROPIC_BASE_URL=https://example.com
# COMMENTED_OUT=value
EMPTY=
BACK2BASE_CLOUD_SYNC=1
`
	os.WriteFile(envFile, []byte(content), 0644)

	vals := readEnvFile(envFile)

	if vals["CLAUDE_CODE_OAUTH_TOKEN"] != "tok123" {
		t.Errorf("token = %q, want tok123", vals["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	if vals["ANTHROPIC_BASE_URL"] != "https://example.com" {
		t.Errorf("base_url = %q", vals["ANTHROPIC_BASE_URL"])
	}
	if _, ok := vals["COMMENTED_OUT"]; ok {
		t.Error("commented key should not be present")
	}
	if vals["EMPTY"] != "" {
		t.Errorf("empty = %q, want empty string", vals["EMPTY"])
	}
	if vals["BACK2BASE_CLOUD_SYNC"] != "1" {
		t.Errorf("cloud_sync = %q, want 1", vals["BACK2BASE_CLOUD_SYNC"])
	}
}

func TestSetEnvValue(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	content := `# Auth
CLAUDE_CODE_OAUTH_TOKEN=old
ANTHROPIC_API_KEY=
`
	os.WriteFile(envFile, []byte(content), 0644)

	err := setEnvValue(envFile, "CLAUDE_CODE_OAUTH_TOKEN", "new-token")
	if err != nil {
		t.Fatal(err)
	}

	vals := readEnvFile(envFile)
	if vals["CLAUDE_CODE_OAUTH_TOKEN"] != "new-token" {
		t.Errorf("token = %q, want new-token", vals["CLAUDE_CODE_OAUTH_TOKEN"])
	}
}
