package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// checkStatus is a tri-state outcome for an individual doctor check.
type checkStatus string

const (
	statusPass checkStatus = "pass"
	statusFail checkStatus = "fail"
	statusSkip checkStatus = "skip"
)

// checkResult is the per-check report row.
type checkResult struct {
	Name    string      `json:"name"`
	Status  checkStatus `json:"status"`
	Detail  string      `json:"detail"`
	FixHint string      `json:"fix_hint,omitempty"`
}

// cmdRunner is the abstraction over `exec.Command(...).Output()` so tests
// can stub out docker invocations. The real implementation honors a
// per-call context timeout.
type cmdRunner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var doctorJSON bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run a battery of health checks across docker, payload, and config",
	Long: `doctor inspects the layers an oss-back2base launch depends on and
reports PASS/FAIL/SKIP per check with fix hints. Use it as the first stop
when something is wrong: it pinpoints which layer (Docker, payload,
settings) actually broke.`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Emit machine-readable JSON instead of text")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	cfg := resolveConfig()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	runner := execRunner{}

	results := collectChecks(ctx, cfg, runner)

	out := cmd.OutOrStdout()
	if doctorJSON {
		if err := renderJSON(out, results); err != nil {
			return err
		}
	} else {
		renderText(out, results)
	}

	if exitCodeFor(results) != 0 {
		// Surface a nonzero exit without printing Cobra's usage banner.
		os.Exit(1)
	}
	return nil
}

// collectChecks runs the full battery in order and returns results.
// Order matters: later checks may skip themselves based on the outcome
// of earlier ones (e.g. image-inspect skips when Docker is unreachable).
func collectChecks(ctx context.Context, cfg cbConfig, runner cmdRunner) []checkResult {
	var results []checkResult

	// 1. Docker reachable
	dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	dockerRes := checkDockerReachable(dctx, runner)
	cancel()
	dockerRes.Name = "Docker reachable"
	results = append(results, dockerRes)

	// 2. Compose available
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	composeRes := checkComposeAvailable(cctx, runner)
	cancel()
	composeRes.Name = "Compose available"
	results = append(results, composeRes)

	// 3. Payload extracted
	payloadRes := checkPayloadExtracted(cfg.Home)
	payloadRes.Name = "Container payload extracted"
	results = append(results, payloadRes)

	// 4. Image exists (skip if docker is down)
	if dockerRes.Status == statusFail {
		results = append(results, checkResult{
			Name:   "Container image exists",
			Status: statusSkip,
			Detail: "Docker not reachable",
		})
	} else {
		ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
		imageRes := checkImageExists(ictx, runner, resolveBaseImage())
		cancel()
		imageRes.Name = "Container image exists"
		results = append(results, imageRes)
	}

	// 5. settings.json parseable
	settingsRes := checkJSONParseable("Settings", filepath.Join(cfg.StateDir, "settings.json"))
	settingsRes.Name = "settings.json parseable"
	results = append(results, settingsRes)

	// 6. .mcp.json parseable
	mcpRes := checkMCPConfig(filepath.Join(cfg.StateDir, ".mcp.json"))
	mcpRes.Name = "MCP config parseable"
	results = append(results, mcpRes)

	// 7. Stale binary
	staleRes := checkStaleBinary(filepath.Join(cfg.ConfigDir, "last-version-check.json"), version)
	staleRes.Name = "Binary up to date"
	results = append(results, staleRes)

	return results
}

// ── individual checks ──────────────────────────────────────────────────

func checkDockerReachable(ctx context.Context, r cmdRunner) checkResult {
	out, err := r.Run(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return checkResult{
			Status:  statusFail,
			Detail:  "Docker daemon not reachable",
			FixHint: "start Docker Desktop / `sudo systemctl start docker`",
		}
	}
	v := strings.TrimSpace(out)
	if v == "" {
		v = "unknown"
	}
	return checkResult{
		Status: statusPass,
		Detail: "server v" + v,
	}
}

func checkComposeAvailable(ctx context.Context, r cmdRunner) checkResult {
	if out, err := r.Run(ctx, "docker", "compose", "version"); err == nil {
		return checkResult{Status: statusPass, Detail: strings.TrimSpace(firstLine(out))}
	}
	// v1 standalone fallback
	if out, err := r.Run(ctx, "docker-compose", "--version"); err == nil {
		return checkResult{Status: statusPass, Detail: "v1 standalone: " + strings.TrimSpace(firstLine(out))}
	}
	return checkResult{
		Status:  statusFail,
		Detail:  "neither `docker compose` nor `docker-compose` available",
		FixHint: "install Docker Desktop or the docker-compose-plugin package",
	}
}

func checkPayloadExtracted(home string) checkResult {
	hashFile := filepath.Join(home, ".extract-hash")
	if _, err := os.Stat(hashFile); err != nil {
		return checkResult{
			Status:  statusFail,
			Detail:  "no .extract-hash at " + home,
			FixHint: "run any back2base command to trigger extraction",
		}
	}
	// Spot-check one file the runtime always needs.
	compose := filepath.Join(home, "docker-compose.yml")
	if _, err := os.Stat(compose); err != nil {
		return checkResult{
			Status:  statusFail,
			Detail:  "missing docker-compose.yml at " + home,
			FixHint: "delete " + hashFile + " and re-run back2base to force re-extraction",
		}
	}
	return checkResult{
		Status: statusPass,
		Detail: "payload at " + home,
	}
}

func checkImageExists(ctx context.Context, r cmdRunner, image string) checkResult {
	if _, err := r.Run(ctx, "docker", "image", "inspect", image); err != nil {
		return checkResult{
			Status:  statusFail,
			Detail:  "image " + image + " not present",
			FixHint: "run `back2base build` or `docker pull " + image + "`",
		}
	}
	return checkResult{
		Status: statusPass,
		Detail: image,
	}
}

func checkJSONParseable(label, path string) checkResult {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return checkResult{
			Status: statusSkip,
			Detail: "no file at " + path,
		}
	}
	if err != nil {
		return checkResult{Status: statusFail, Detail: "read " + path + ": " + err.Error()}
	}
	var anyVal any
	if err := json.Unmarshal(data, &anyVal); err != nil {
		return checkResult{
			Status:  statusFail,
			Detail:  label + " has invalid JSON: " + err.Error(),
			FixHint: "fix or delete " + path,
		}
	}
	return checkResult{Status: statusPass, Detail: path + " parses"}
}

func checkMCPConfig(path string) checkResult {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return checkResult{Status: statusSkip, Detail: "no file at " + path}
	}
	if err != nil {
		return checkResult{Status: statusFail, Detail: err.Error()}
	}
	var parsed struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return checkResult{
			Status:  statusFail,
			Detail:  "invalid JSON: " + err.Error(),
			FixHint: "fix or delete " + path,
		}
	}
	return checkResult{
		Status: statusPass,
		Detail: fmt.Sprintf("%d server(s) configured", len(parsed.MCPServers)),
	}
}

func checkStaleBinary(cachePath, currentVersion string) checkResult {
	data, err := os.ReadFile(cachePath)
	if os.IsNotExist(err) {
		return checkResult{
			Status: statusSkip,
			Detail: "no version-check cache yet",
		}
	}
	if err != nil {
		return checkResult{Status: statusFail, Detail: err.Error()}
	}
	var parsed struct {
		Latest string `json:"latest"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return checkResult{Status: statusFail, Detail: "invalid version-check cache: " + err.Error()}
	}
	if parsed.Latest == "" || parsed.Latest == currentVersion {
		return checkResult{
			Status: statusPass,
			Detail: "running " + currentVersion,
		}
	}
	return checkResult{
		Status:  statusFail,
		Detail:  fmt.Sprintf("running %s; latest is %s", currentVersion, parsed.Latest),
		FixHint: "run `oss-back2base selfupdate` (or upgrade via your package manager)",
	}
}

// ── render & summarize ─────────────────────────────────────────────────

func summarize(results []checkResult) (pass, fail, skip int) {
	for _, r := range results {
		switch r.Status {
		case statusPass:
			pass++
		case statusFail:
			fail++
		case statusSkip:
			skip++
		}
	}
	return
}

func exitCodeFor(results []checkResult) int {
	for _, r := range results {
		if r.Status == statusFail {
			return 1
		}
	}
	return 0
}

func renderText(w io.Writer, results []checkResult) {
	for _, r := range results {
		tag := "[PASS]"
		switch r.Status {
		case statusFail:
			tag = "[FAIL]"
		case statusSkip:
			tag = "[SKIP]"
		}
		line := fmt.Sprintf("%s %s: %s", tag, r.Name, r.Detail)
		if r.FixHint != "" && r.Status == statusFail {
			line += " (" + r.FixHint + ")"
		}
		fmt.Fprintln(w, line)
	}
	p, f, s := summarize(results)
	fmt.Fprintln(w)
	fmt.Fprintf(w, ":: %d passed, %d failed, %d skipped\n", p, f, s)
}

func renderJSON(w io.Writer, results []checkResult) error {
	payload := struct {
		Checks []checkResult `json:"checks"`
	}{Checks: results}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// firstLine returns the first non-empty line of s, without trailing whitespace.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}
