package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestExtractAssets(t *testing.T) {
	testFS := fstest.MapFS{
		"Dockerfile":              {Data: []byte("FROM node:24\n")},
		"entrypoint.sh":           {Data: []byte("#!/bin/bash\necho hi\n")},
		"defaults/mcp.json":       {Data: []byte(`{"mcpServers":{}}` + "\n")},
		"lib/session-snapshot.sh": {Data: []byte("#!/bin/bash\n")},
	}

	dest := t.TempDir()
	extracted, err := extractFS(testFS, dest, "test-hash")
	if err != nil {
		t.Fatal(err)
	}
	if !extracted {
		t.Error("expected extracted=true on first run")
	}

	for _, name := range []string{
		"Dockerfile",
		"entrypoint.sh",
		"defaults/mcp.json",
		"lib/session-snapshot.sh",
	} {
		path := filepath.Join(dest, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	hashFile := filepath.Join(dest, ".extract-hash")
	data, err := os.ReadFile(hashFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "test-hash" {
		t.Errorf("hash = %q, want test-hash", string(data))
	}
}

func TestExtractSkipsWhenCurrent(t *testing.T) {
	testFS := fstest.MapFS{
		"Dockerfile": {Data: []byte("FROM node:24\n")},
	}

	dest := t.TempDir()
	_, _ = extractFS(testFS, dest, "hash-v1")

	canary := filepath.Join(dest, "canary.txt")
	os.WriteFile(canary, []byte("should survive"), 0644)

	extracted, err := extractFS(testFS, dest, "hash-v1")
	if err != nil {
		t.Fatal(err)
	}
	if extracted {
		t.Error("expected extracted=false when hash matches")
	}

	if _, err := os.Stat(canary); err != nil {
		t.Error("canary was removed — extract should have been skipped")
	}
}

func TestExtractOverwritesOnNewHash(t *testing.T) {
	testFS := fstest.MapFS{
		"Dockerfile": {Data: []byte("FROM node:24\n")},
	}

	dest := t.TempDir()
	_, _ = extractFS(testFS, dest, "hash-v1")

	_, err := extractFS(testFS, dest, "hash-v2")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dest, ".extract-hash"))
	if string(data) != "hash-v2" {
		t.Errorf("hash = %q, want hash-v2", string(data))
	}
}

func TestSeedEnvFile(t *testing.T) {
	testFS := fstest.MapFS{
		"defaults/env.example": {Data: []byte("# config\nBACK2BASE_CLAUDE_CODE_OAUTH_TOKEN=\n")},
	}

	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")

	seeded, err := seedEnvFile(testFS, envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !seeded {
		t.Error("expected seeded=true on first run")
	}

	data, _ := os.ReadFile(envPath)
	if len(data) == 0 {
		t.Error("env file is empty")
	}

	os.WriteFile(envPath, []byte("CUSTOM=1\n"), 0644)
	seeded, err = seedEnvFile(testFS, envPath)
	if err != nil {
		t.Fatal(err)
	}
	if seeded {
		t.Error("expected seeded=false when file exists")
	}
	data, _ = os.ReadFile(envPath)
	if string(data) != "CUSTOM=1\n" {
		t.Error("existing env file was overwritten")
	}
}
