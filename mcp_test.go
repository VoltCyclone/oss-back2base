package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// ── loadMCPConfig ──────────────────────────────────────────────────────────

func TestLoadMCPConfig_FromStateDir(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(statePath, []byte(`{
		"mcpServers": {
			"foo": {"type": "http", "url": "https://example.com"}
		}
	}`), 0644); err != nil {
		t.Fatal(err)
	}

	embedFS := fstest.MapFS{
		"defaults/mcp.json": {Data: []byte(`{"mcpServers": {"bar": {}}}`)},
	}

	cfg, src, err := loadMCPConfig(dir, embedFS)
	if err != nil {
		t.Fatal(err)
	}
	if src != mcpSourceState {
		t.Errorf("source = %v, want state", src)
	}
	if _, ok := cfg.MCPServers["foo"]; !ok {
		t.Errorf("expected foo from state, got %#v", cfg.MCPServers)
	}
	if _, ok := cfg.MCPServers["bar"]; ok {
		t.Errorf("should not have bar from embed when state present")
	}
}

func TestLoadMCPConfig_EmbeddedFallback(t *testing.T) {
	dir := t.TempDir() // no .mcp.json in here
	embedFS := fstest.MapFS{
		"defaults/mcp.json": {Data: []byte(`{
			"mcpServers": {
				"bar": {"type": "stdio", "command": "echo"}
			}
		}`)},
	}

	cfg, src, err := loadMCPConfig(dir, embedFS)
	if err != nil {
		t.Fatal(err)
	}
	if src != mcpSourceEmbedded {
		t.Errorf("source = %v, want embedded", src)
	}
	if _, ok := cfg.MCPServers["bar"]; !ok {
		t.Errorf("expected bar from embed, got %#v", cfg.MCPServers)
	}
}

func TestLoadMCPConfig_BothMissing(t *testing.T) {
	dir := t.TempDir()
	embedFS := fstest.MapFS{}
	_, _, err := loadMCPConfig(dir, embedFS)
	if err == nil {
		t.Fatal("expected error when neither state nor embed have mcp.json")
	}
}

func TestLoadMCPConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	embedFS := fstest.MapFS{}
	_, _, err := loadMCPConfig(dir, embedFS)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ── mcpServerInfo extraction ───────────────────────────────────────────────

func TestServerInfos_Stdio(t *testing.T) {
	cfg := mcpConfig{
		MCPServers: map[string]mcpServer{
			"git": {Command: "mcp-server-git", Args: []string{}},
		},
	}
	infos := serverInfos(cfg)
	if len(infos) != 1 {
		t.Fatalf("got %d infos, want 1", len(infos))
	}
	got := infos[0]
	if got.Name != "git" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Transport != "stdio" {
		t.Errorf("transport = %q, want stdio (default)", got.Transport)
	}
	if got.Display != "mcp-server-git" {
		t.Errorf("display = %q, want mcp-server-git", got.Display)
	}
}

func TestServerInfos_StdioWithArgs(t *testing.T) {
	cfg := mcpConfig{
		MCPServers: map[string]mcpServer{
			"sqlite": {Command: "mcp-server-sqlite", Args: []string{"/workspace/data.db"}},
		},
	}
	infos := serverInfos(cfg)
	got := infos[0]
	if got.Display != "mcp-server-sqlite /workspace/data.db" {
		t.Errorf("display = %q", got.Display)
	}
}

func TestServerInfos_Npx(t *testing.T) {
	cfg := mcpConfig{
		MCPServers: map[string]mcpServer{
			"foo": {Command: "npx", Args: []string{"-y", "@scope/pkg-name"}},
		},
	}
	infos := serverInfos(cfg)
	got := infos[0]
	if got.Display != "npx @scope/pkg-name" {
		t.Errorf("display = %q, want npx @scope/pkg-name", got.Display)
	}
}

func TestServerInfos_Docker(t *testing.T) {
	cfg := mcpConfig{
		MCPServers: map[string]mcpServer{
			"buildkite": {
				Type:    "stdio",
				Command: "docker",
				Args:    []string{"run", "-q", "-i", "--rm", "-e", "BK_TOKEN", "buildkite/mcp-server", "stdio"},
			},
		},
	}
	infos := serverInfos(cfg)
	got := infos[0]
	if got.Transport != "stdio" {
		t.Errorf("transport = %q", got.Transport)
	}
	if !strings.HasPrefix(got.Display, "docker") {
		t.Errorf("display = %q, want to start with docker", got.Display)
	}
	if !strings.Contains(got.Display, "buildkite/mcp-server") {
		t.Errorf("display = %q, want it to contain image name", got.Display)
	}
}

func TestServerInfos_HTTP(t *testing.T) {
	cfg := mcpConfig{
		MCPServers: map[string]mcpServer{
			"aws-knowledge": {Type: "http", URL: "https://knowledge-mcp.global.api.aws"},
		},
	}
	infos := serverInfos(cfg)
	got := infos[0]
	if got.Transport != "http" {
		t.Errorf("transport = %q, want http", got.Transport)
	}
	if got.Display != "https://knowledge-mcp.global.api.aws" {
		t.Errorf("display = %q", got.Display)
	}
	if got.URL != "https://knowledge-mcp.global.api.aws" {
		t.Errorf("URL = %q", got.URL)
	}
}

func TestServerInfos_EnvKeysFiltering(t *testing.T) {
	cfg := mcpConfig{
		MCPServers: map[string]mcpServer{
			"a": {Command: "x", Env: map[string]string{
				"REAL":  "${TOKEN}",
				"EMPTY": "",
			}},
			"b": {Command: "x"}, // no env at all
			"c": {Command: "x", Env: map[string]string{
				"K1": "v1",
				"K2": "v2",
				"K3": "",
			}},
		},
	}
	infos := serverInfos(cfg)

	byName := make(map[string]mcpServerInfo, len(infos))
	for _, i := range infos {
		byName[i.Name] = i
	}

	if got := byName["a"].EnvRequired; len(got) != 1 || got[0] != "REAL" {
		t.Errorf("a envs = %v, want [REAL]", got)
	}
	if got := byName["b"].EnvRequired; len(got) != 0 {
		t.Errorf("b envs = %v, want []", got)
	}
	got := byName["c"].EnvRequired
	if len(got) != 2 || got[0] != "K1" || got[1] != "K2" {
		t.Errorf("c envs = %v, want sorted [K1 K2]", got)
	}
}

func TestServerInfos_SortedByName(t *testing.T) {
	cfg := mcpConfig{
		MCPServers: map[string]mcpServer{
			"zeta":  {Command: "x"},
			"alpha": {Command: "y"},
			"beta":  {Command: "z"},
		},
	}
	infos := serverInfos(cfg)
	want := []string{"alpha", "beta", "zeta"}
	for i, n := range want {
		if infos[i].Name != n {
			t.Errorf("infos[%d].Name = %q, want %q", i, infos[i].Name, n)
		}
	}
}

// ── renderMCPList ──────────────────────────────────────────────────────────

func TestRunMCPList_TableSourceLineState(t *testing.T) {
	infos := []mcpServerInfo{
		{Name: "foo", Transport: "stdio", Display: "echo", EnvRequired: nil},
	}
	var buf bytes.Buffer
	if err := renderMCPList(&buf, infos, mcpSourceState, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "source: ~/.claude/.mcp.json") {
		t.Errorf("missing state source line:\n%s", out)
	}
}

func TestRunMCPList_TableSourceLineEmbed(t *testing.T) {
	var buf bytes.Buffer
	if err := renderMCPList(&buf, nil, mcpSourceEmbedded, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "embedded defaults") {
		t.Errorf("missing embedded source line:\n%s", buf.String())
	}
}

func TestRunMCPList_TableHeaders(t *testing.T) {
	infos := []mcpServerInfo{
		{Name: "git", Transport: "stdio", Display: "mcp-server-git"},
		{Name: "aws", Transport: "http", Display: "https://x.example", URL: "https://x.example"},
	}
	var buf bytes.Buffer
	if err := renderMCPList(&buf, infos, mcpSourceState, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, h := range []string{"NAME", "TRANSPORT", "COMMAND/URL", "ENV REQUIRED"} {
		if !strings.Contains(out, h) {
			t.Errorf("missing header %q in:\n%s", h, out)
		}
	}
	if !strings.Contains(out, "git") || !strings.Contains(out, "aws") {
		t.Errorf("missing rows in:\n%s", out)
	}
}

func TestRunMCPList_EnvCellRendersJoined(t *testing.T) {
	infos := []mcpServerInfo{
		{Name: "memory", Transport: "stdio", Display: "memory-mcp",
			EnvRequired: []string{"MEMORY_MCP_TOKEN", "MEMORY_NAMESPACE"}},
	}
	var buf bytes.Buffer
	if err := renderMCPList(&buf, infos, mcpSourceState, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "MEMORY_MCP_TOKEN") || !strings.Contains(out, "MEMORY_NAMESPACE") {
		t.Errorf("missing env keys in row:\n%s", out)
	}
}

func TestRunMCPList_JSON(t *testing.T) {
	infos := []mcpServerInfo{
		{Name: "git", Transport: "stdio", Display: "mcp-server-git"},
		{Name: "aws", Transport: "http", Display: "https://x.example", URL: "https://x.example"},
	}
	var buf bytes.Buffer
	if err := renderMCPList(&buf, infos, mcpSourceState, true); err != nil {
		t.Fatal(err)
	}

	var parsed struct {
		Source  string          `json:"source"`
		Servers []mcpServerInfo `json:"servers"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if parsed.Source != "state" {
		t.Errorf("Source = %q", parsed.Source)
	}
	if len(parsed.Servers) != 2 {
		t.Errorf("Servers = %d, want 2", len(parsed.Servers))
	}
}

// ── renderMCPTest / probeAll ───────────────────────────────────────────────

func TestRunMCPTest_HTTPPass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	infos := []mcpServerInfo{
		{Name: "ok", Transport: "http", Display: srv.URL, URL: srv.URL},
	}
	results := probeAll(infos, "", srv.Client(), 3*time.Second)
	var buf bytes.Buffer
	if err := renderMCPTest(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "[PASS]") {
		t.Errorf("expected PASS, got:\n%s", out)
	}
	if !strings.Contains(out, "ok (http)") {
		t.Errorf("expected ok (http) in output:\n%s", out)
	}
}

func TestRunMCPTest_HTTP4xxStillPass(t *testing.T) {
	// Many MCP HTTP endpoints reject unauth requests with 401/403,
	// but reachability is what we're proving.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	infos := []mcpServerInfo{
		{Name: "auth", Transport: "http", Display: srv.URL, URL: srv.URL},
	}
	results := probeAll(infos, "", srv.Client(), 3*time.Second)
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].Status != "PASS" {
		t.Errorf("status = %q, want PASS (4xx still proves reachability)", results[0].Status)
	}
}

func TestRunMCPTest_HTTPFail(t *testing.T) {
	// Unrouteable address; should fail with a network error.
	infos := []mcpServerInfo{
		{Name: "dead", Transport: "http", Display: "http://127.0.0.1:1", URL: "http://127.0.0.1:1"},
	}
	results := probeAll(infos, "", &http.Client{Timeout: 500 * time.Millisecond}, 500*time.Millisecond)
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].Status != "FAIL" {
		t.Errorf("status = %q, want FAIL", results[0].Status)
	}
}

func TestRunMCPTest_StdioSkipsWithoutNetwork(t *testing.T) {
	// Use a client whose transport panics if invoked. Stdio servers must
	// not trigger any network call at all.
	failClient := &http.Client{
		Transport: roundTripperFn(func(req *http.Request) (*http.Response, error) {
			t.Fatalf("stdio server caused HTTP request to %s", req.URL)
			return nil, nil
		}),
	}

	infos := []mcpServerInfo{
		{Name: "memory", Transport: "stdio", Display: "memory-mcp"},
	}
	results := probeAll(infos, "", failClient, 3*time.Second)
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].Status != "SKIP" {
		t.Errorf("status = %q, want SKIP for stdio", results[0].Status)
	}
	if !strings.Contains(results[0].Reason, "stdio") {
		t.Errorf("reason = %q, want it to mention stdio", results[0].Reason)
	}
}

type roundTripperFn func(*http.Request) (*http.Response, error)

func (f roundTripperFn) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRunMCPTest_ServerFilter(t *testing.T) {
	infos := []mcpServerInfo{
		{Name: "a", Transport: "stdio", Display: "x"},
		{Name: "b", Transport: "stdio", Display: "y"},
	}
	results := probeAll(infos, "b", http.DefaultClient, 3*time.Second)
	if len(results) != 1 {
		t.Fatalf("got %d, want 1 (filter b)", len(results))
	}
	if results[0].Name != "b" {
		t.Errorf("name = %q, want b", results[0].Name)
	}
}

func TestRunMCPTest_Summary(t *testing.T) {
	results := []mcpProbeResult{
		{Name: "a", Transport: "http", Status: "PASS"},
		{Name: "b", Transport: "stdio", Status: "SKIP"},
		{Name: "c", Transport: "http", Status: "FAIL"},
	}
	var buf bytes.Buffer
	if err := renderMCPTest(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "1 passed") || !strings.Contains(out, "1 failed") || !strings.Contains(out, "1 skipped") {
		t.Errorf("missing summary in:\n%s", out)
	}
}

func TestRunMCPTest_JSON(t *testing.T) {
	results := []mcpProbeResult{
		{Name: "a", Transport: "http", Status: "PASS", HTTPStatus: 200, DurationMS: 42},
		{Name: "b", Transport: "stdio", Status: "SKIP", Reason: "stdio — runs only inside container"},
	}
	var buf bytes.Buffer
	if err := renderMCPTest(&buf, results, true); err != nil {
		t.Fatal(err)
	}

	var parsed struct {
		Results []mcpProbeResult `json:"results"`
		Summary struct {
			Passed  int `json:"passed"`
			Failed  int `json:"failed"`
			Skipped int `json:"skipped"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(parsed.Results) != 2 {
		t.Errorf("len(Results) = %d, want 2", len(parsed.Results))
	}
	if parsed.Summary.Passed != 1 || parsed.Summary.Skipped != 1 || parsed.Summary.Failed != 0 {
		t.Errorf("summary = %+v", parsed.Summary)
	}
}

func TestExitCodeFromResults(t *testing.T) {
	pass := []mcpProbeResult{{Status: "PASS"}, {Status: "SKIP"}}
	if exitCodeFromResults(pass) != 0 {
		t.Errorf("all-pass should be 0")
	}
	fail := []mcpProbeResult{{Status: "PASS"}, {Status: "FAIL"}}
	if exitCodeFromResults(fail) == 0 {
		t.Errorf("any-fail should be non-zero")
	}
}
