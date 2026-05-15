package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// updateInfo is the result of a successful (cached or live) check that
// indicates the running binary is older than the latest released version.
type updateInfo struct {
	Current string
	Latest  string
}

// versionCheckCache is what we persist between invocations to avoid
// hitting the GitHub API more than once a day.
type versionCheckCache struct {
	CheckedAt     string `json:"checked_at"`
	LatestVersion string `json:"latest_version"`
}

const (
	updateCheckCacheFile = "last-version-check.json"
	updateCheckTTL       = 24 * time.Hour
)

// checkForUpdates is the package-level entry point used from
// PersistentPreRunE. It reads the cached latest-version (refreshing if
// stale) and returns non-nil iff the running binary is behind. Network
// problems are silent: callers must not log on a nil return.
func checkForUpdates() *updateInfo {
	return checkForUpdatesWith(version, releaseURL, 3*time.Second)
}

// checkForUpdatesWith is the testable form of checkForUpdates. The
// baseURL is the GitHub releases-latest endpoint; tests inject an
// httptest server URL.
func checkForUpdatesWith(currentVersion, baseURL string, timeout time.Duration) *updateInfo {
	if os.Getenv("OSS_BACK2BASE_NO_UPDATE_CHECK") != "" {
		return nil
	}
	if currentVersion == "" || currentVersion == "dev" {
		return nil
	}

	cfg := resolveConfig()
	cachePath := filepath.Join(cfg.StateDir, updateCheckCacheFile)

	latest, ok := readFreshCache(cachePath)
	if !ok {
		fetched, err := fetchLatestTag(baseURL, timeout)
		if err != nil || fetched == "" {
			return nil
		}
		latest = fetched
		_ = writeCache(cachePath, versionCheckCache{
			CheckedAt:     time.Now().UTC().Format(time.RFC3339),
			LatestVersion: latest,
		})
	}

	if latest == "" {
		return nil
	}

	latestTrim := parseReleaseTag(latest)
	if !isNewer(currentVersion, latestTrim) {
		return nil
	}

	return &updateInfo{
		Current: currentVersion,
		Latest:  latest,
	}
}

// readFreshCache returns (version, true) if a non-stale cache entry
// exists at path; otherwise ("", false).
func readFreshCache(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var c versionCheckCache
	if err := json.Unmarshal(data, &c); err != nil {
		return "", false
	}
	if c.LatestVersion == "" {
		return "", false
	}
	t, err := time.Parse(time.RFC3339, c.CheckedAt)
	if err != nil {
		return "", false
	}
	if time.Since(t) >= updateCheckTTL {
		return "", false
	}
	return c.LatestVersion, true
}

// writeCache persists a versionCheckCache atomically(ish) to path,
// creating the parent directory if missing.
func writeCache(path string, c versionCheckCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// fetchLatestTag hits the GitHub releases-latest endpoint and returns
// the bare tag string (e.g. "v1.2.3"). All errors are returned silently
// to the caller; callers must not log them.
func fetchLatestTag(baseURL string, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(baseURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &httpStatusError{Status: resp.StatusCode}
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

type httpStatusError struct{ Status int }

func (e *httpStatusError) Error() string {
	return http.StatusText(e.Status)
}
