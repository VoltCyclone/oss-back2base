package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// baseComposeArgs returns the docker compose flags shared by every
// oss-back2base subcommand. extraFiles, if any, are added as additional `-f`
// flags AFTER the base compose file so their values override the base
// (compose's documented merge order).
func baseComposeArgs(cfg cbConfig, extraFiles ...string) []string {
	args := []string{
		"compose",
		"-f", filepath.Join(cfg.Home, "docker-compose.yml"),
	}
	for _, f := range extraFiles {
		args = append(args, "-f", f)
	}
	args = append(args,
		"--env-file", cfg.EnvFile,
		"--project-directory", cfg.Home,
	)
	return args
}

// hostCredsOverridePath is where writeHostCredsOverride stages its
// generated compose override fragment.
func hostCredsOverridePath(cfg cbConfig) string {
	return filepath.Join(cfg.StateDir, "run", "host-creds-override.yml")
}

// writeHostCredsOverride generates a docker-compose override that
// bind-mounts the host's per-tool config dirs (~/.aws, ~/.kube,
// ~/.config/gh) at sidecar paths inside the container, but only for
// dirs that actually exist on the host.
//
// The base docker-compose.yml does NOT declare these mounts because
// docker-compose's auto-create-host-path behavior would otherwise leave
// empty config dirs in $HOME for users who don't have those tools
// installed. Generating the override per-run also means a tool the user
// installs between runs (e.g. `aws configure` after first launch) is
// picked up on the next start without any oss-back2base-side reconfig.
//
// Returns the override path on success, "" if no host creds were found
// (no override written), or "" if writing failed (caller falls through
// to no override; container starts without those tools' creds staged).
func writeHostCredsOverride(cfg cbConfig) string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	type mount struct{ src, dst string }
	candidates := []mount{
		{filepath.Join(home, ".aws"), "/home/node/.aws-host"},
		{filepath.Join(home, ".kube"), "/home/node/.kube-host"},
		{filepath.Join(home, ".config", "gh"), "/home/node/.config/gh-host"},
	}
	var lines []string
	for _, m := range candidates {
		fi, err := os.Stat(m.src)
		if err != nil || !fi.IsDir() {
			continue
		}
		lines = append(lines, fmt.Sprintf("      - %s:%s:ro", m.src, m.dst))
	}
	if len(lines) == 0 {
		return ""
	}
	body := "services:\n  claude:\n    volumes:\n" + strings.Join(lines, "\n") + "\n"
	path := hostCredsOverridePath(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, ":: warn: could not stage compose override dir (%v)\n", err)
		return ""
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, ":: warn: could not write compose override (%v)\n", err)
		return ""
	}
	return path
}

// managedSettingsHostDir returns the platform-specific directory Claude
// Code reads enterprise / managed-policy files from on the host, or ""
// when the platform isn't supported. BACK2BASE_MANAGED_SETTINGS_DIR
// overrides the probe (useful for sandboxed environments, dev setups,
// or admins who deploy policy at a non-standard path).
//
// References (docs.claude.com):
//   - macOS:  /Library/Application Support/ClaudeCode/
//   - Linux:  /etc/claude-code/
//
// Within that directory we look for three artifacts:
//   - managed-settings.json   — top-tier policy file
//   - managed-settings.d/     — drop-in directory for modular policies
//   - CLAUDE.md               — admin-deployed context prepended to memory
func managedSettingsHostDir() string {
	if v := strings.TrimSpace(os.Getenv("BACK2BASE_MANAGED_SETTINGS_DIR")); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "linux":
		return "/etc/claude-code"
	default:
		return ""
	}
}

// managedSettingsOverridePath is where writeManagedSettingsOverride stages
// its generated compose override fragment.
func managedSettingsOverridePath(cfg cbConfig) string {
	return filepath.Join(cfg.StateDir, "run", "managed-settings-override.yml")
}

// writeManagedSettingsOverride generates a docker-compose override that
// bind-mounts the host's Claude Code managed-policy artifacts at the
// canonical Linux paths inside the container. Claude Code reads these
// paths unconditionally (no env-var or flag exists to redirect them), so
// a bind mount is the only way enterprise policy gets honored when the
// CLI runs in a sandbox.
//
// All mounts are read-only and only emitted for artifacts that actually
// exist on the host. Returns the override path on success or "" if no
// managed artifacts were found.
func writeManagedSettingsOverride(cfg cbConfig) string {
	hostDir := managedSettingsHostDir()
	if hostDir == "" {
		return ""
	}
	type mount struct{ src, dst string }
	candidates := []mount{
		{filepath.Join(hostDir, "managed-settings.json"), "/etc/claude-code/managed-settings.json"},
		{filepath.Join(hostDir, "managed-settings.d"), "/etc/claude-code/managed-settings.d"},
		{filepath.Join(hostDir, "CLAUDE.md"), "/etc/claude-code/CLAUDE.md"},
	}
	var lines []string
	for _, m := range candidates {
		if _, err := os.Stat(m.src); err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("      - %q:%q:ro", m.src, m.dst))
	}
	if len(lines) == 0 {
		return ""
	}
	body := "services:\n  claude:\n    volumes:\n" + strings.Join(lines, "\n") + "\n"
	path := managedSettingsOverridePath(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, ":: warn: could not stage managed-settings override dir (%v)\n", err)
		return ""
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, ":: warn: could not write managed-settings override (%v)\n", err)
		return ""
	}
	return path
}

type runOpts struct {
	extraDirs []string
	prompt    string
	model     string
	resumeID  string
}

func buildRunArgs(cfg cbConfig, opts runOpts) []string {
	var overrides []string
	if p := writeHostCredsOverride(cfg); p != "" {
		overrides = append(overrides, p)
	}
	if p := writeManagedSettingsOverride(cfg); p != "" {
		overrides = append(overrides, p)
	}
	args := baseComposeArgs(cfg, overrides...)
	args = append(args, "run", "--rm")

	var addDirs []string
	for _, d := range opts.extraDirs {
		name := filepath.Base(d)
		args = append(args, "-v", d+":/repos/"+name)
		addDirs = append(addDirs, "/repos/"+name)
	}

	args = append(args,
		"claude",
		"claude",
		"--permission-mode", "bypassPermissions",
		"--dangerously-skip-permissions",
		"--mcp-config", "/home/node/.claude/.mcp.json",
	)

	if opts.resumeID != "" {
		args = append(args, "--resume", opts.resumeID)
	}

	if opts.model != "" {
		args = append(args, "--model", opts.model)
	}

	for _, d := range addDirs {
		args = append(args, "--add-dir", d)
	}

	if opts.prompt != "" {
		args = append(args, "-p", opts.prompt)
	}

	return args
}

func composeExec(args []string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found in PATH: %w", err)
	}

	env := composeEnv()
	return syscall.Exec(dockerPath, append([]string{"docker"}, args...), env)
}

func composeRun(args []string) error {
	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = composeEnv()
	return cmd.Run()
}

// composeEnv augments the caller's environment with values needed by
// docker-compose interpolation. The OSS build has no auth/proxy/cloud
// integrations, so this just pins the base image.
func composeEnv() []string {
	env := os.Environ()
	if os.Getenv("BACK2BASE_BASE_IMAGE") == "" {
		env = append(env, "BACK2BASE_BASE_IMAGE="+resolveBaseImage())
	}
	return env
}
