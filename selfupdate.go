package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
)

const releaseURL = "https://api.github.com/repos/voltcyclone/oss-back2base/releases/latest"

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func parseReleaseTag(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

func isNewer(current, latest string) bool {
	if current == "dev" && latest != "dev" {
		return true
	}
	if latest == "dev" {
		return false
	}

	cp := parseSemver(current)
	lp := parseSemver(latest)

	for i := 0; i < 3; i++ {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		result[i], _ = strconv.Atoi(parts[i])
	}
	return result
}

func checkForUpdate() (newVersion, downloadURL string, err error) {
	resp, err := http.Get(releaseURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}

	latest := parseReleaseTag(release.TagName)
	if !isNewer(version, latest) {
		return "", "", nil
	}

	wantName := fmt.Sprintf("back2base_%s_%s", runtime.GOOS, runtime.GOARCH)
	for _, a := range release.Assets {
		if strings.Contains(a.Name, wantName) {
			return latest, a.BrowserDownloadURL, nil
		}
	}

	return latest, "", fmt.Errorf("no binary found for %s/%s in release %s",
		runtime.GOOS, runtime.GOARCH, release.TagName)
}

func doSelfUpdate(downloadURL string) error {
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return err
	}

	// GoReleaser produces tar.gz archives. Extract the binary from within.
	var binaryReader io.Reader
	if strings.HasSuffix(downloadURL, ".tar.gz") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("decompress: %w", err)
		}
		defer gr.Close()

		tr := tar.NewReader(gr)
		found := false
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("read tar: %w", err)
			}
			if hdr.Name == "back2base" || strings.HasSuffix(hdr.Name, "/back2base") {
				binaryReader = tr
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("back2base binary not found in archive")
		}
	} else {
		binaryReader = resp.Body
	}

	tmpFile := executablePath + ".new"
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(f, binaryReader); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("write: %w", err)
	}
	f.Close()

	if err := os.Rename(tmpFile, executablePath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("replace binary: %w", err)
	}

	return nil
}

// isHomebrewInstall returns true if the binary is installed under a
// Homebrew prefix (e.g. /opt/homebrew, /usr/local/Cellar).
func isHomebrewInstall() bool {
	p, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(p, "Cellar") || strings.Contains(p, "homebrew")
}

// isAptInstall returns true if the binary appears to be installed via
// a Debian package (dpkg/apt), i.e. lives under /usr/bin or /usr/local/bin
// and dpkg reports the package as installed.
func isAptInstall() bool {
	p, err := os.Executable()
	if err != nil {
		return false
	}
	if p != "/usr/bin/oss-back2base" && p != "/usr/local/bin/oss-back2base" {
		return false
	}
	// Check if dpkg knows about the package
	if _, err := os.Stat("/var/lib/dpkg/info/oss-back2base.list"); err == nil {
		return true
	}
	return false
}
