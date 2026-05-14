package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// ── Types ──────────────────────────────────────────────────────────────────

type mcpServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type mcpConfig struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

type mcpSource int

const (
	mcpSourceState mcpSource = iota
	mcpSourceEmbedded
)

func (s mcpSource) String() string {
	if s == mcpSourceState {
		return "state"
	}
	return "embedded"
}

// mcpServerInfo is the table-and-JSON-friendly per-server view.
type mcpServerInfo struct {
	Name        string   `json:"name"`
	Transport   string   `json:"transport"`
	Display     string   `json:"display"`
	URL         string   `json:"url,omitempty"`
	EnvRequired []string `json:"env_required"`
}

// mcpProbeResult is one row of `back2base mcp test` output.
type mcpProbeResult struct {
	Name       string `json:"name"`
	Transport  string `json:"transport"`
	Status     string `json:"status"` // PASS | FAIL | SKIP
	HTTPStatus int    `json:"http_status,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Reason     string `json:"reason,omitempty"`
	URL        string `json:"url,omitempty"`
}

// ── Loader ─────────────────────────────────────────────────────────────────

// loadMCPConfig reads <stateDir>/.mcp.json (the bind-mounted user-state
// version maintained by past container runs). If that file is absent it
// falls back to the binary-embedded defaults under defaults/mcp.json.
func loadMCPConfig(stateDir string, assets fs.FS) (mcpConfig, mcpSource, error) {
	statePath := filepath.Join(stateDir, ".mcp.json")
	if data, err := os.ReadFile(statePath); err == nil {
		cfg, perr := parseMCPConfig(data)
		if perr != nil {
			return mcpConfig{}, 0, fmt.Errorf("parse %s: %w", statePath, perr)
		}
		return cfg, mcpSourceState, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return mcpConfig{}, 0, fmt.Errorf("read %s: %w", statePath, err)
	}

	data, err := fs.ReadFile(assets, "defaults/mcp.json")
	if err != nil {
		return mcpConfig{}, 0, fmt.Errorf("no state .mcp.json and no embedded defaults: %w", err)
	}
	cfg, perr := parseMCPConfig(data)
	if perr != nil {
		return mcpConfig{}, 0, fmt.Errorf("parse embedded mcp.json: %w", perr)
	}
	return cfg, mcpSourceEmbedded, nil
}

func parseMCPConfig(data []byte) (mcpConfig, error) {
	var cfg mcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return mcpConfig{}, err
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]mcpServer{}
	}
	return cfg, nil
}

// ── Per-server projection ─────────────────────────────────────────────────

// serverInfos returns one mcpServerInfo per server, sorted by name. Mirrors
// the bin_display / env_keys logic in entrypoint.sh (lines 503-528).
func serverInfos(cfg mcpConfig) []mcpServerInfo {
	names := make([]string, 0, len(cfg.MCPServers))
	for n := range cfg.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]mcpServerInfo, 0, len(names))
	for _, name := range names {
		s := cfg.MCPServers[name]
		transport := s.Type
		if transport == "" {
			transport = "stdio"
		}

		info := mcpServerInfo{
			Name:        name,
			Transport:   transport,
			EnvRequired: extractEnvKeys(s.Env),
		}

		switch transport {
		case "http":
			info.URL = s.URL
			info.Display = s.URL
		default: // stdio
			info.Display = stdioDisplay(s)
		}
		out = append(out, info)
	}
	return out
}

// stdioDisplay reproduces entrypoint.sh's bin_display for stdio servers:
//   - npx: "npx <first-non-flag-arg-starting-with-@-or-letter>"
//   - docker: "docker <first-image-like-arg>"
//   - other: "command [args[0]]"
func stdioDisplay(s mcpServer) string {
	switch s.Command {
	case "npx":
		pkg := firstNpxPackage(s.Args)
		if pkg == "" {
			return "npx"
		}
		return "npx " + pkg
	case "docker":
		img := firstDockerImage(s.Args)
		if img == "" {
			return "docker"
		}
		return "docker " + img
	default:
		if len(s.Args) > 0 && s.Args[0] != "" {
			return s.Command + " " + s.Args[0]
		}
		return s.Command
	}
}

// firstNpxPackage finds the first arg that looks like a package — starts
// with "@" or a lowercase letter, and is not a flag. Mirrors the jq filter
// at entrypoint.sh:478-480.
func firstNpxPackage(args []string) string {
	for _, a := range args {
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		first := a[0]
		if first == '@' || (first >= 'a' && first <= 'z') {
			return a
		}
	}
	return ""
}

// firstDockerImage matches `select(test("^[a-z].*/"))` — the first arg
// shaped like an OCI repo (lowercase, contains a slash).
func firstDockerImage(args []string) string {
	for _, a := range args {
		if len(a) == 0 {
			continue
		}
		if a[0] >= 'a' && a[0] <= 'z' && strings.Contains(a, "/") {
			return a
		}
	}
	return ""
}

// extractEnvKeys returns sorted keys whose value is non-empty (and not
// nil — but our typed map can't hold nil values, so empty-string is the
// only falsy case we filter).
func extractEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k, v := range env {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// ── Renderers ──────────────────────────────────────────────────────────────

func renderMCPList(w io.Writer, infos []mcpServerInfo, src mcpSource, asJSON bool) error {
	if asJSON {
		payload := struct {
			Source  string          `json:"source"`
			Servers []mcpServerInfo `json:"servers"`
		}{
			Source:  src.String(),
			Servers: infos,
		}
		// Ensure non-nil for clean JSON output.
		if payload.Servers == nil {
			payload.Servers = []mcpServerInfo{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	switch src {
	case mcpSourceState:
		fmt.Fprintln(w, "source: ~/.claude/.mcp.json")
	default:
		fmt.Fprintln(w, "source: embedded defaults (no state yet)")
	}
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTRANSPORT\tCOMMAND/URL\tENV REQUIRED")
	for _, i := range infos {
		envCell := strings.Join(i.EnvRequired, ", ")
		if envCell == "" {
			envCell = "-"
		}
		display := truncate(i.Display, 60)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", i.Name, i.Transport, display, envCell)
	}
	return tw.Flush()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func renderMCPTest(w io.Writer, results []mcpProbeResult, asJSON bool) error {
	passed, failed, skipped := tally(results)

	if asJSON {
		payload := struct {
			Results []mcpProbeResult `json:"results"`
			Summary struct {
				Passed  int `json:"passed"`
				Failed  int `json:"failed"`
				Skipped int `json:"skipped"`
			} `json:"summary"`
		}{Results: results}
		payload.Summary.Passed = passed
		payload.Summary.Failed = failed
		payload.Summary.Skipped = skipped
		if payload.Results == nil {
			payload.Results = []mcpProbeResult{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	for _, r := range results {
		switch r.Status {
		case "PASS":
			fmt.Fprintf(w, "[PASS] %s (%s): %d %s in %dms\n",
				r.Name, r.Transport, r.HTTPStatus, http.StatusText(r.HTTPStatus), r.DurationMS)
		case "SKIP":
			fmt.Fprintf(w, "[SKIP] %s (%s): %s\n", r.Name, r.Transport, r.Reason)
		case "FAIL":
			fmt.Fprintf(w, "[FAIL] %s (%s): %s\n", r.Name, r.Transport, r.Reason)
		}
	}
	fmt.Fprintf(w, "\n%d passed, %d failed, %d skipped\n", passed, failed, skipped)
	return nil
}

func tally(results []mcpProbeResult) (passed, failed, skipped int) {
	for _, r := range results {
		switch r.Status {
		case "PASS":
			passed++
		case "FAIL":
			failed++
		case "SKIP":
			skipped++
		}
	}
	return
}

func exitCodeFromResults(results []mcpProbeResult) int {
	for _, r := range results {
		if r.Status == "FAIL" {
			return 1
		}
	}
	return 0
}

// ── Probing ────────────────────────────────────────────────────────────────

// probeAll runs the reachability probe for each server. Stdio servers always
// return SKIP without performing any network call. http servers probe the URL
// (HEAD, falling back to GET if the server rejects HEAD with 4xx/5xx).
//
// `serverFilter`, when non-empty, restricts the run to a single server name.
func probeAll(infos []mcpServerInfo, serverFilter string, client *http.Client, timeout time.Duration) []mcpProbeResult {
	results := make([]mcpProbeResult, 0, len(infos))
	for _, info := range infos {
		if serverFilter != "" && info.Name != serverFilter {
			continue
		}
		results = append(results, probeOne(info, client, timeout))
	}
	return results
}

func probeOne(info mcpServerInfo, client *http.Client, timeout time.Duration) mcpProbeResult {
	r := mcpProbeResult{
		Name:      info.Name,
		Transport: info.Transport,
		URL:       info.URL,
	}

	if info.Transport != "http" {
		// Stdio (and anything else that's not http) is intentionally
		// untestable from the host — actually running it would require
		// spawning docker/npx/whatever inside the container.
		r.Status = "SKIP"
		r.Reason = "stdio — runs only inside container"
		return r
	}

	if info.URL == "" {
		r.Status = "FAIL"
		r.Reason = "http transport with empty URL"
		return r
	}

	status, dur, err := httpProbe(info.URL, client, timeout)
	r.DurationMS = dur.Milliseconds()
	if err != nil {
		r.Status = "FAIL"
		r.Reason = err.Error()
		return r
	}
	r.HTTPStatus = status
	// Any non-network response — including 401/403 — proves reachability.
	r.Status = "PASS"
	return r
}

// httpProbe sends HEAD; if the server replies "method not allowed" or similar
// (405/501), it retries with GET. Either way, returning a status code (any
// status) means the host was reachable.
func httpProbe(url string, client *http.Client, timeout time.Duration) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		// HEAD rejected with 405/501 → try GET; otherwise return as-is.
		if resp.StatusCode != 405 && resp.StatusCode != 501 {
			return resp.StatusCode, time.Since(start), nil
		}
	}

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, time.Since(start), err
	}
	getResp, err := client.Do(getReq)
	if err != nil {
		return 0, time.Since(start), err
	}
	defer getResp.Body.Close()
	return getResp.StatusCode, time.Since(start), nil
}

// ── Cobra wiring ───────────────────────────────────────────────────────────

var (
	flagMCPJSON   bool
	flagMCPServer string
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Inspect configured MCP servers",
	Long: `Surface the MCP servers configured for the current namespace and check
their reachability without entering the container.

Subcommands:
  list   Pretty-print configured servers (name, transport, command/URL, env).
  test   Probe HTTP-transport servers; stdio is skipped (it only runs inside
         the container).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured MCP servers",
	Args:  cobra.NoArgs,
	RunE:  runMCPList,
}

var mcpTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Probe MCP-server reachability (HTTP only; stdio skipped)",
	Args:  cobra.NoArgs,
	RunE:  runMCPTest,
}

func init() {
	mcpListCmd.Flags().BoolVar(&flagMCPJSON, "json", false, "Emit machine-readable JSON")
	mcpTestCmd.Flags().BoolVar(&flagMCPJSON, "json", false, "Emit machine-readable JSON")
	mcpTestCmd.Flags().StringVar(&flagMCPServer, "server", "", "Test only the named server (default: all)")

	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpTestCmd)
	rootCmd.AddCommand(mcpCmd)
}

func runMCPList(cmd *cobra.Command, args []string) error {
	cfg := resolveConfig()
	mcp, src, err := loadMCPConfig(cfg.StateDir, shipFS())
	if err != nil {
		return err
	}
	return renderMCPList(os.Stdout, serverInfos(mcp), src, flagMCPJSON)
}

func runMCPTest(cmd *cobra.Command, args []string) error {
	cfg := resolveConfig()
	mcp, _, err := loadMCPConfig(cfg.StateDir, shipFS())
	if err != nil {
		return err
	}

	infos := serverInfos(mcp)
	if flagMCPServer != "" {
		found := false
		for _, info := range infos {
			if info.Name == flagMCPServer {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no MCP server named %q in current config", flagMCPServer)
		}
	}

	client := &http.Client{Timeout: 3 * time.Second}
	results := probeAll(infos, flagMCPServer, client, 3*time.Second)

	if err := renderMCPTest(os.Stdout, results, flagMCPJSON); err != nil {
		return err
	}
	if code := exitCodeFromResults(results); code != 0 {
		os.Exit(code)
	}
	return nil
}
