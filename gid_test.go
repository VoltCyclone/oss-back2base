package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGIDOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
		ok     bool
	}{
		{"normal", "991\n", "991", true},
		{"root", "0\n", "0", true},
		{"no newline", "999", "999", true},
		{"empty", "", "", false},
		{"error text", "stat: /var/run/docker.sock: No such file\n", "", false},
		{"whitespace", "  991  \n", "991", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseGIDOutput(tt.output)
			if ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("gid = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGIDCacheFallback(t *testing.T) {
	// Disable live probe so we exercise the cache path
	orig := probeGIDFunc
	probeGIDFunc = func() string { return "" }
	defer func() { probeGIDFunc = orig }()

	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "docker-gid")
	os.WriteFile(cacheFile, []byte("991"), 0644)

	gid := resolveGID("", cacheFile)
	if gid != "991" {
		t.Errorf("gid = %q, want 991 from cache", gid)
	}
}

func TestGIDDefault(t *testing.T) {
	orig := probeGIDFunc
	probeGIDFunc = func() string { return "" }
	defer func() { probeGIDFunc = orig }()

	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "docker-gid") // doesn't exist

	gid := resolveGID("", cacheFile)
	if gid != "999" {
		t.Errorf("gid = %q, want 999 default", gid)
	}
}

func TestGIDEnvOverride(t *testing.T) {
	gid := resolveGID("42", "/nonexistent")
	if gid != "42" {
		t.Errorf("gid = %q, want 42 from env", gid)
	}
}
