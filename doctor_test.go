package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner implements cmdRunner for tests so we never spawn `docker`.
type fakeRunner struct {
	// resultFor returns (stdout, err) keyed by command + first arg.
	resultFor map[string]fakeResult
}

type fakeResult struct {
	out string
	err error
}

func (f fakeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	key := name
	if len(args) > 0 {
		key = name + " " + strings.Join(args, " ")
	}
	if r, ok := f.resultFor[key]; ok {
		return r.out, r.err
	}
	// Try a prefix match (e.g. "docker version").
	for k, r := range f.resultFor {
		if strings.HasPrefix(key, k) {
			return r.out, r.err
		}
	}
	return "", fmt.Errorf("fakeRunner: no stub for %q", key)
}

func TestCheckDockerReachable_Pass(t *testing.T) {
	r := fakeRunner{resultFor: map[string]fakeResult{
		"docker version": {out: "25.0.3\n", err: nil},
	}}
	res := checkDockerReachable(context.Background(), r)
	if res.Status != statusPass {
		t.Fatalf("expected PASS, got %s: %s", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "25.0.3") {
		t.Errorf("expected detail to contain version, got %q", res.Detail)
	}
}

func TestCheckDockerReachable_Fail(t *testing.T) {
	r := fakeRunner{resultFor: map[string]fakeResult{
		"docker version": {out: "", err: fmt.Errorf("Cannot connect")},
	}}
	res := checkDockerReachable(context.Background(), r)
	if res.Status != statusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}
	if res.FixHint == "" {
		t.Errorf("expected fix hint on docker fail")
	}
}

func TestCheckComposeAvailable_Pass(t *testing.T) {
	r := fakeRunner{resultFor: map[string]fakeResult{
		"docker compose version": {out: "Docker Compose version v2.27.0\n"},
	}}
	res := checkComposeAvailable(context.Background(), r)
	if res.Status != statusPass {
		t.Fatalf("expected PASS, got %s: %s", res.Status, res.Detail)
	}
}

func TestCheckComposeAvailable_FailFallsBackToV1(t *testing.T) {
	// v2 missing, v1 standalone present
	r := fakeRunner{resultFor: map[string]fakeResult{
		"docker compose version": {err: fmt.Errorf("unknown command")},
		"docker-compose":          {out: "docker-compose version 1.29\n"},
	}}
	res := checkComposeAvailable(context.Background(), r)
	if res.Status != statusPass {
		t.Fatalf("expected PASS via v1 fallback, got %s", res.Status)
	}
}

func TestCheckComposeAvailable_FailBoth(t *testing.T) {
	r := fakeRunner{resultFor: map[string]fakeResult{
		"docker compose version": {err: fmt.Errorf("nope")},
		"docker-compose":          {err: fmt.Errorf("not found")},
	}}
	res := checkComposeAvailable(context.Background(), r)
	if res.Status != statusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}
}

func TestCheckPayloadExtracted_Pass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".extract-hash"), []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}"), 0644); err != nil {
		t.Fatal(err)
	}
	res := checkPayloadExtracted(dir)
	if res.Status != statusPass {
		t.Fatalf("expected PASS, got %s: %s", res.Status, res.Detail)
	}
}

func TestCheckPayloadExtracted_FailMissingHash(t *testing.T) {
	dir := t.TempDir()
	res := checkPayloadExtracted(dir)
	if res.Status != statusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}
}

func TestCheckPayloadExtracted_FailMissingCompose(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, ".extract-hash"), []byte("abc"), 0644)
	res := checkPayloadExtracted(dir)
	if res.Status != statusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}
}

func TestCheckImageExists_Pass(t *testing.T) {
	r := fakeRunner{resultFor: map[string]fakeResult{
		"docker image inspect": {out: `[{"Id":"sha256:..."}]`, err: nil},
	}}
	res := checkImageExists(context.Background(), r, "img:latest")
	if res.Status != statusPass {
		t.Fatalf("expected PASS, got %s: %s", res.Status, res.Detail)
	}
}

func TestCheckImageExists_Fail(t *testing.T) {
	r := fakeRunner{resultFor: map[string]fakeResult{
		"docker image inspect": {err: fmt.Errorf("No such image")},
	}}
	res := checkImageExists(context.Background(), r, "img:latest")
	if res.Status != statusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}
	if res.FixHint == "" {
		t.Errorf("expected fix hint")
	}
}

func TestCheckSettingsParseable_Pass(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(`{"theme":"dark"}`), 0644); err != nil {
		t.Fatal(err)
	}
	res := checkJSONParseable("Settings", p)
	if res.Status != statusPass {
		t.Fatalf("expected PASS, got %s: %s", res.Status, res.Detail)
	}
}

func TestCheckSettingsParseable_Skip(t *testing.T) {
	dir := t.TempDir()
	res := checkJSONParseable("Settings", filepath.Join(dir, "missing.json"))
	if res.Status != statusSkip {
		t.Fatalf("expected SKIP, got %s", res.Status)
	}
}

func TestCheckSettingsParseable_FailInvalid(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(p, []byte(`{not json`), 0644); err != nil {
		t.Fatal(err)
	}
	res := checkJSONParseable("Settings", p)
	if res.Status != statusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}
}

func TestCheckMCPConfig_ServerCount(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".mcp.json")
	body := `{"mcpServers":{"a":{},"b":{},"c":{}}}`
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	res := checkMCPConfig(p)
	if res.Status != statusPass {
		t.Fatalf("expected PASS, got %s: %s", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "3") {
		t.Errorf("expected server count of 3 in detail, got %q", res.Detail)
	}
}

func TestCheckStaleBinary_NoCacheSkips(t *testing.T) {
	dir := t.TempDir()
	res := checkStaleBinary(filepath.Join(dir, "missing.json"), "v0.5.0")
	if res.Status != statusSkip {
		t.Fatalf("expected SKIP, got %s", res.Status)
	}
}

func TestCheckStaleBinary_BehindFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "lvc.json")
	if err := os.WriteFile(p, []byte(`{"latest":"v0.9.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	res := checkStaleBinary(p, "v0.5.0")
	if res.Status != statusFail {
		t.Fatalf("expected FAIL when behind, got %s: %s", res.Status, res.Detail)
	}
}

func TestCheckStaleBinary_UpToDatePasses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "lvc.json")
	_ = os.WriteFile(p, []byte(`{"latest":"v0.5.0"}`), 0644)
	res := checkStaleBinary(p, "v0.5.0")
	if res.Status != statusPass {
		t.Fatalf("expected PASS, got %s", res.Status)
	}
}

// Aggregator tests.

func TestSummary_Counts(t *testing.T) {
	results := []checkResult{
		{Name: "a", Status: statusPass},
		{Name: "b", Status: statusPass},
		{Name: "c", Status: statusFail},
		{Name: "d", Status: statusSkip},
	}
	p, f, s := summarize(results)
	if p != 2 || f != 1 || s != 1 {
		t.Errorf("expected 2/1/1, got %d/%d/%d", p, f, s)
	}
}

func TestExitCode_NoFailuresZero(t *testing.T) {
	results := []checkResult{
		{Name: "a", Status: statusPass},
		{Name: "b", Status: statusSkip},
	}
	if code := exitCodeFor(results); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestExitCode_AnyFailureOne(t *testing.T) {
	results := []checkResult{
		{Name: "a", Status: statusPass},
		{Name: "b", Status: statusFail},
	}
	if code := exitCodeFor(results); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestRenderJSON_Shape(t *testing.T) {
	results := []checkResult{
		{Name: "Docker", Status: statusPass, Detail: "v25"},
		{Name: "Auth", Status: statusFail, Detail: "missing", FixHint: "run login"},
	}
	var buf bytes.Buffer
	if err := renderJSON(&buf, results); err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Checks []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Detail  string `json:"detail"`
			FixHint string `json:"fix_hint"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v\nraw=%s", err, buf.String())
	}
	if len(decoded.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(decoded.Checks))
	}
	if decoded.Checks[0].Name != "Docker" || decoded.Checks[0].Status != "pass" {
		t.Errorf("unexpected first check: %+v", decoded.Checks[0])
	}
	if decoded.Checks[1].FixHint != "run login" {
		t.Errorf("expected fix_hint preserved, got %q", decoded.Checks[1].FixHint)
	}
}

func TestRenderText_LinesAndSummary(t *testing.T) {
	results := []checkResult{
		{Name: "Docker", Status: statusPass, Detail: "v25"},
		{Name: "Auth", Status: statusFail, Detail: "missing", FixHint: "run login"},
		{Name: "Cloud", Status: statusSkip, Detail: "disabled"},
	}
	var buf bytes.Buffer
	renderText(&buf, results)
	out := buf.String()
	if !strings.Contains(out, "[PASS]") {
		t.Errorf("expected [PASS] in output:\n%s", out)
	}
	if !strings.Contains(out, "[FAIL]") {
		t.Errorf("expected [FAIL] in output:\n%s", out)
	}
	if !strings.Contains(out, "[SKIP]") {
		t.Errorf("expected [SKIP] in output:\n%s", out)
	}
	if !strings.Contains(out, "1 passed, 1 failed, 1 skipped") {
		t.Errorf("expected summary line, got:\n%s", out)
	}
	if !strings.Contains(out, "run login") {
		t.Errorf("expected fix hint in output:\n%s", out)
	}
}
