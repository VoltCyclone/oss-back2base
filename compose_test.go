package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaseComposeArgs(t *testing.T) {
	cfg := cbConfig{
		Home:      "/opt/back2base",
		ConfigDir: "/etc/back2base",
		StateDir:  "/etc/back2base/state",
		EnvFile:   "/etc/back2base/env",
	}

	args := baseComposeArgs(cfg)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-f /opt/back2base/docker-compose.yml") {
		t.Errorf("missing compose file flag: %v", args)
	}
	if !strings.Contains(joined, "--env-file /etc/back2base/env") {
		t.Errorf("missing env-file flag: %v", args)
	}
	if !strings.Contains(joined, "--project-directory /opt/back2base") {
		t.Errorf("missing project-directory flag: %v", args)
	}
}

func TestBuildRunArgs(t *testing.T) {
	cfg := cbConfig{
		Home:      "/opt/back2base",
		ConfigDir: "/etc/back2base",
		StateDir:  "/etc/back2base/state",
		EnvFile:   "/etc/back2base/env",
	}

	args := buildRunArgs(cfg, runOpts{
		extraDirs: []string{"/home/user/proj1", "/home/user/proj2"},
		prompt:    "write tests",
		model:     "claude-opus-4-6",
	})

	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-v /home/user/proj1:/repos/proj1") {
		t.Errorf("missing proj1 mount: %v", args)
	}
	if !strings.Contains(joined, "-v /home/user/proj2:/repos/proj2") {
		t.Errorf("missing proj2 mount: %v", args)
	}
	if !strings.Contains(joined, "--mcp-config /home/node/.claude/.mcp.json") {
		t.Errorf("missing mcp-config flag: %v", args)
	}
	if !strings.Contains(joined, "-p write tests") {
		t.Errorf("missing prompt: %v", args)
	}
	if !strings.Contains(joined, "--model claude-opus-4-6") {
		t.Errorf("missing model: %v", args)
	}
	if !strings.Contains(joined, "--add-dir /repos/proj1") {
		t.Errorf("missing add-dir flag: %v", args)
	}
}

func TestBuildRunArgsMinimal(t *testing.T) {
	cfg := cbConfig{
		Home:      "/opt/back2base",
		ConfigDir: "/etc/back2base",
		StateDir:  "/etc/back2base/state",
		EnvFile:   "/etc/back2base/env",
	}

	args := buildRunArgs(cfg, runOpts{})

	joined := strings.Join(args, " ")

	if strings.Contains(joined, " -p ") {
		t.Errorf("unexpected -p flag: %v", args)
	}
	if strings.Contains(joined, "--model") {
		t.Errorf("unexpected --model flag: %v", args)
	}
	if strings.Contains(joined, "--add-dir") {
		t.Errorf("unexpected --add-dir flag: %v", args)
	}
	if !strings.Contains(joined, "--permission-mode bypassPermissions") {
		t.Errorf("missing permission-mode: %v", args)
	}
}

func TestBuildRunArgs_ResumeIDAppendsFlag(t *testing.T) {
	cfg := cbConfig{
		Home: "/opt/back2base", ConfigDir: "/c", StateDir: "/c/state", EnvFile: "/c/env",
	}
	args := buildRunArgs(cfg, runOpts{resumeID: "abc-123"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--resume abc-123") {
		t.Fatalf("expected --resume abc-123 in args: %v", args)
	}
	idxResume := strings.Index(joined, "--resume")
	idxSkip := strings.Index(joined, "--dangerously-skip-permissions")
	if idxResume < idxSkip {
		t.Fatalf("--resume should follow --dangerously-skip-permissions: %s", joined)
	}
}

func TestBuildRunArgs_NoResumeIDNoFlag(t *testing.T) {
	cfg := cbConfig{Home: "/opt/back2base", ConfigDir: "/c", StateDir: "/c/state", EnvFile: "/c/env"}
	args := buildRunArgs(cfg, runOpts{})
	for _, a := range args {
		if a == "--resume" {
			t.Fatalf("unexpected --resume flag: %v", args)
		}
	}
}

func TestBuildRunArgs_ResumeIDAndModelOrder(t *testing.T) {
	cfg := cbConfig{Home: "/opt/back2base", ConfigDir: "/c", StateDir: "/c/state", EnvFile: "/c/env"}
	args := buildRunArgs(cfg, runOpts{resumeID: "abc-123", model: "claude-opus-4-7"})

	// Both flags present.
	var idxResume, idxModel = -1, -1
	for i, a := range args {
		switch a {
		case "--resume":
			idxResume = i
		case "--model":
			idxModel = i
		}
	}
	if idxResume == -1 {
		t.Fatalf("missing --resume in args: %v", args)
	}
	if idxModel == -1 {
		t.Fatalf("missing --model in args: %v", args)
	}
	// --resume before --model: matches the build order (resume is appended
	// right after --dangerously-skip-permissions, model is appended after).
	if idxResume > idxModel {
		t.Fatalf("--resume should come before --model: %v", args)
	}
	// Each flag must be followed by its value.
	if args[idxResume+1] != "abc-123" {
		t.Fatalf("--resume not followed by id: %v", args)
	}
	if args[idxModel+1] != "claude-opus-4-7" {
		t.Fatalf("--model not followed by value: %v", args)
	}
}

// TestWriteHostCredsOverride_NoDirsReturnsEmpty: when none of the host
// config dirs exist, the override is not written and the function
// returns an empty string so callers fall back to no override.
func TestWriteHostCredsOverride_NoDirsReturnsEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	tmpState := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cfg := cbConfig{StateDir: tmpState}

	got := writeHostCredsOverride(cfg)
	if got != "" {
		t.Fatalf("expected empty string when no host dirs exist, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(tmpState, "run", "host-creds-override.yml")); !os.IsNotExist(err) {
		t.Fatalf("override file should not exist: err=%v", err)
	}
}

// TestWriteHostCredsOverride_PartialIncludesOnlyExisting: only the
// host dirs that actually exist get bind mounts in the generated
// override file.
func TestWriteHostCredsOverride_PartialIncludesOnlyExisting(t *testing.T) {
	tmpHome := t.TempDir()
	tmpState := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpHome, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpHome, ".config", "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	// .kube intentionally absent.
	t.Setenv("HOME", tmpHome)
	cfg := cbConfig{StateDir: tmpState}

	path := writeHostCredsOverride(cfg)
	if path == "" {
		t.Fatal("expected non-empty override path when host dirs exist")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read override: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, ".aws:/home/node/.aws-host:ro") {
		t.Errorf("missing aws mount: %s", s)
	}
	if !strings.Contains(s, ".config/gh:/home/node/.config/gh-host:ro") {
		t.Errorf("missing gh mount: %s", s)
	}
	if strings.Contains(s, "kube-host") {
		t.Errorf("kube mount should be absent: %s", s)
	}
}

// TestWriteHostCredsOverride_FileOnlyIsIgnored: a regular file at
// ~/.aws (not a dir) does not produce a bind mount.
func TestWriteHostCredsOverride_FileOnlyIsIgnored(t *testing.T) {
	tmpHome := t.TempDir()
	tmpState := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpHome, ".aws"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)
	cfg := cbConfig{StateDir: tmpState}

	got := writeHostCredsOverride(cfg)
	if got != "" {
		t.Fatalf("expected empty when ~/.aws is a file, got %q", got)
	}
}

// TestBuildRunArgs_AppendsHostCredsOverrideWhenPresent: when host
// creds dirs exist, buildRunArgs threads the generated override into
// the compose command via an extra -f flag, AFTER the base compose
// file.
func TestBuildRunArgs_AppendsHostCredsOverrideWhenPresent(t *testing.T) {
	tmpHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpHome, ".kube"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)

	tmpState := t.TempDir()
	cfg := cbConfig{
		Home:      "/opt/back2base",
		ConfigDir: "/etc/back2base",
		StateDir:  tmpState,
		EnvFile:   "/etc/back2base/env",
	}

	args := buildRunArgs(cfg, runOpts{})
	joined := strings.Join(args, " ")

	overridePath := filepath.Join(tmpState, "run", "host-creds-override.yml")
	if !strings.Contains(joined, "-f "+overridePath) {
		t.Fatalf("expected -f %s in args: %v", overridePath, args)
	}
	idxBase := strings.Index(joined, "/opt/back2base/docker-compose.yml")
	idxOverride := strings.Index(joined, overridePath)
	if idxBase < 0 || idxOverride < 0 || idxOverride < idxBase {
		t.Fatalf("override -f must follow base compose -f: %s", joined)
	}
}

// TestWriteManagedSettingsOverride_NoDirReturnsEmpty: when the host
// managed-settings dir doesn't exist (or is empty of the three known
// artifacts), no override is written.
func TestWriteManagedSettingsOverride_NoDirReturnsEmpty(t *testing.T) {
	t.Setenv("BACK2BASE_MANAGED_SETTINGS_DIR", filepath.Join(t.TempDir(), "does-not-exist"))
	cfg := cbConfig{StateDir: t.TempDir()}
	if got := writeManagedSettingsOverride(cfg); got != "" {
		t.Fatalf("expected empty when dir is absent, got %q", got)
	}
}

// TestWriteManagedSettingsOverride_PartialIncludesOnlyExisting: only the
// managed-settings artifacts that actually exist on the host get bind
// mounts; missing ones are silently skipped.
func TestWriteManagedSettingsOverride_PartialIncludesOnlyExisting(t *testing.T) {
	hostDir := t.TempDir()
	// managed-settings.json present, CLAUDE.md present, managed-settings.d absent.
	if err := os.WriteFile(filepath.Join(hostDir, "managed-settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostDir, "CLAUDE.md"), []byte("# policy"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACK2BASE_MANAGED_SETTINGS_DIR", hostDir)
	cfg := cbConfig{StateDir: t.TempDir()}

	path := writeManagedSettingsOverride(cfg)
	if path == "" {
		t.Fatal("expected non-empty override path when artifacts exist")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read override: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "/etc/claude-code/managed-settings.json") {
		t.Errorf("override missing managed-settings.json mount target:\n%s", body)
	}
	if !strings.Contains(body, "/etc/claude-code/CLAUDE.md") {
		t.Errorf("override missing CLAUDE.md mount target:\n%s", body)
	}
	if strings.Contains(body, "managed-settings.d") {
		t.Errorf("override unexpectedly references managed-settings.d (host dir absent):\n%s", body)
	}
	if !strings.Contains(body, ":ro") {
		t.Errorf("override mount must be read-only:\n%s", body)
	}
}

// TestBuildRunArgs_AppendsManagedSettingsOverrideWhenPresent: when host
// managed-settings artifacts exist, buildRunArgs threads the generated
// override into the compose command alongside (and after) the base
// compose file.
func TestBuildRunArgs_AppendsManagedSettingsOverrideWhenPresent(t *testing.T) {
	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "managed-settings.json"), []byte(`{"permissions":{"deny":["Bash"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACK2BASE_MANAGED_SETTINGS_DIR", hostDir)
	// HOME isolation so the host-creds probe doesn't add unrelated overrides.
	t.Setenv("HOME", t.TempDir())

	tmpState := t.TempDir()
	cfg := cbConfig{
		Home:      "/opt/back2base",
		ConfigDir: "/etc/back2base",
		StateDir:  tmpState,
		EnvFile:   "/etc/back2base/env",
	}

	args := buildRunArgs(cfg, runOpts{})
	joined := strings.Join(args, " ")

	overridePath := filepath.Join(tmpState, "run", "managed-settings-override.yml")
	if !strings.Contains(joined, "-f "+overridePath) {
		t.Fatalf("expected -f %s in args: %v", overridePath, args)
	}
	idxBase := strings.Index(joined, "/opt/back2base/docker-compose.yml")
	idxOverride := strings.Index(joined, overridePath)
	if idxBase < 0 || idxOverride < 0 || idxOverride < idxBase {
		t.Fatalf("managed-settings override -f must follow base compose -f: %s", joined)
	}
}
