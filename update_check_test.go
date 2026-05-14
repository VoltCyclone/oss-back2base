package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempStateDir points the state-dir at a temp directory for the duration
// of the test by setting BACK2BASE_CONFIG.
func withTempStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BACK2BASE_CONFIG", dir)
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	return stateDir
}

func writeTestCache(t *testing.T, stateDir string, c versionCheckCache) {
	t.Helper()
	path := filepath.Join(stateDir, "last-version-check.json")
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
}

func readCache(t *testing.T, stateDir string) versionCheckCache {
	t.Helper()
	path := filepath.Join(stateDir, "last-version-check.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var c versionCheckCache
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return c
}

// fakeReleaseServer returns an httptest server that responds with a fixed tag.
func fakeReleaseServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckForUpdates_FreshCacheUsedWithoutHTTP(t *testing.T) {
	stateDir := withTempStateDir(t)

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9"})
	}))
	defer srv.Close()

	writeTestCache(t, stateDir, versionCheckCache{
		CheckedAt:     time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		LatestVersion: "v1.2.3",
	})

	info := checkForUpdatesWith("0.1.0", srv.URL, 3*time.Second)
	if info == nil {
		t.Fatal("expected updateInfo (latest 1.2.3 > 0.1.0), got nil")
	}
	if info.Latest != "v1.2.3" {
		t.Errorf("Latest = %q, want v1.2.3", info.Latest)
	}
	if hits != 0 {
		t.Errorf("HTTP hit %d times, want 0 (fresh cache)", hits)
	}
}

func TestCheckForUpdates_StaleCacheTriggersHTTP(t *testing.T) {
	stateDir := withTempStateDir(t)

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.0.0"})
	}))
	defer srv.Close()

	writeTestCache(t, stateDir, versionCheckCache{
		CheckedAt:     time.Now().Add(-48 * time.Hour).Format(time.RFC3339),
		LatestVersion: "v1.0.0",
	})

	info := checkForUpdatesWith("1.5.0", srv.URL, 3*time.Second)
	if hits != 1 {
		t.Errorf("HTTP hit %d times, want 1 (stale cache)", hits)
	}
	if info == nil {
		t.Fatal("expected updateInfo (2.0.0 > 1.5.0), got nil")
	}
	if info.Latest != "v2.0.0" {
		t.Errorf("Latest = %q, want v2.0.0", info.Latest)
	}

	// Cache should have been updated.
	got := readCache(t, stateDir)
	if got.LatestVersion != "v2.0.0" {
		t.Errorf("cache LatestVersion = %q, want v2.0.0", got.LatestVersion)
	}
}

func TestCheckForUpdates_HTTPFailureSilent(t *testing.T) {
	withTempStateDir(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	info := checkForUpdatesWith("1.0.0", srv.URL, 3*time.Second)
	if info != nil {
		t.Errorf("expected nil on HTTP error, got %+v", info)
	}
}

func TestCheckForUpdates_VersionEqualReturnsNil(t *testing.T) {
	withTempStateDir(t)
	srv := fakeReleaseServer(t, "v1.2.3")

	info := checkForUpdatesWith("1.2.3", srv.URL, 3*time.Second)
	if info != nil {
		t.Errorf("expected nil when versions equal, got %+v", info)
	}
}

func TestCheckForUpdates_VersionDifferentReturnsInfo(t *testing.T) {
	withTempStateDir(t)
	srv := fakeReleaseServer(t, "v2.0.0")

	info := checkForUpdatesWith("1.0.0", srv.URL, 3*time.Second)
	if info == nil {
		t.Fatal("expected updateInfo, got nil")
	}
	if info.Current != "1.0.0" {
		t.Errorf("Current = %q, want 1.0.0", info.Current)
	}
	if info.Latest != "v2.0.0" {
		t.Errorf("Latest = %q, want v2.0.0", info.Latest)
	}
}

func TestCheckForUpdates_NoUpdateCheckEnvSkips(t *testing.T) {
	withTempStateDir(t)
	t.Setenv("OSS_BACK2BASE_NO_UPDATE_CHECK", "1")

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9"})
	}))
	defer srv.Close()

	info := checkForUpdatesWith("1.0.0", srv.URL, 3*time.Second)
	if info != nil {
		t.Errorf("expected nil with OSS_BACK2BASE_NO_UPDATE_CHECK set, got %+v", info)
	}
	if hits != 0 {
		t.Errorf("HTTP hit %d times, want 0 (env skip)", hits)
	}
}

func TestCheckForUpdates_CacheRoundTrip(t *testing.T) {
	stateDir := withTempStateDir(t)
	srv := fakeReleaseServer(t, "v3.4.5")

	// First call: no cache, fetches and writes cache.
	_ = checkForUpdatesWith("1.0.0", srv.URL, 3*time.Second)

	got := readCache(t, stateDir)
	if got.LatestVersion != "v3.4.5" {
		t.Errorf("cache LatestVersion = %q, want v3.4.5", got.LatestVersion)
	}
	parsed, err := time.Parse(time.RFC3339, got.CheckedAt)
	if err != nil {
		t.Errorf("CheckedAt parse: %v", err)
	}
	if time.Since(parsed) > time.Minute {
		t.Errorf("CheckedAt %v older than expected", parsed)
	}
}

func TestCheckForUpdates_DevBuildSkipped(t *testing.T) {
	withTempStateDir(t)

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9"})
	}))
	defer srv.Close()

	if info := checkForUpdatesWith("dev", srv.URL, 3*time.Second); info != nil {
		t.Errorf("expected nil for dev build, got %+v", info)
	}
	if info := checkForUpdatesWith("", srv.URL, 3*time.Second); info != nil {
		t.Errorf("expected nil for empty version, got %+v", info)
	}
	if hits != 0 {
		t.Errorf("HTTP hit %d times, want 0 (dev build skipped)", hits)
	}
}

// Sanity: ensure the public checkForUpdates() entry point doesn't panic
// even when no env/cache is set up. (HTTP failure path: no real net call
// because we redirect via env? It uses real URL — so we just confirm dev
// build short-circuits before any HTTP.)
func TestCheckForUpdates_PublicEntryDevShortCircuit(t *testing.T) {
	withTempStateDir(t)
	// version global defaults to "dev" in tests.
	if got := checkForUpdates(); got != nil {
		t.Errorf("expected nil for dev version, got %+v", got)
	}
}

// Avoid unused import lints if helpers aren't referenced (not the case
// here, but keep fmt available for diagnostic prints).
var _ = fmt.Sprintf
