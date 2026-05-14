package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// --- Task A: critical-files hash + rebuild decision -------------------------

func TestCriticalFilesHash_Deterministic(t *testing.T) {
	fsA := fstest.MapFS{
		"Dockerfile":          {Data: []byte("FROM node:24\n")},
		"entrypoint.sh":       {Data: []byte("#!/bin/bash\necho hi\n")},
		"init-firewall.sh":    {Data: []byte("#!/bin/bash\n")},
		"lib/cloud-sync.sh":   {Data: []byte("#!/bin/bash\n")},
		"lib/session-snap.sh": {Data: []byte("#!/bin/bash\n")},
		// Non-critical files must be excluded from the hash.
		"defaults/mcp.json": {Data: []byte(`{"a":1}`)},
		"commands/foo.md":   {Data: []byte("# foo")},
		"skills/bar.md":     {Data: []byte("# bar")},
	}

	h1, err := criticalFilesHash(fsA)
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	h2, err := criticalFilesHash(fsA)
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("non-deterministic hash: %s vs %s", h1, h2)
	}
	if h1 == "" {
		t.Fatal("empty hash")
	}
}

func TestCriticalFilesHash_IgnoresNonCriticalChanges(t *testing.T) {
	base := fstest.MapFS{
		"Dockerfile":        {Data: []byte("FROM node:24\n")},
		"entrypoint.sh":     {Data: []byte("#!/bin/bash\n")},
		"init-firewall.sh":  {Data: []byte("#!/bin/bash\n")},
		"lib/cloud-sync.sh": {Data: []byte("#!/bin/bash\n")},
		"defaults/mcp.json": {Data: []byte(`{"a":1}`)},
	}
	h1, err := criticalFilesHash(base)
	if err != nil {
		t.Fatal(err)
	}

	// Modify only non-critical assets.
	mutated := fstest.MapFS{
		"Dockerfile":        {Data: []byte("FROM node:24\n")},
		"entrypoint.sh":     {Data: []byte("#!/bin/bash\n")},
		"init-firewall.sh":  {Data: []byte("#!/bin/bash\n")},
		"lib/cloud-sync.sh": {Data: []byte("#!/bin/bash\n")},
		"defaults/mcp.json": {Data: []byte(`{"a":2}`)}, // changed
		"commands/new.md":   {Data: []byte("new")},      // added
	}
	h2, err := criticalFilesHash(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("non-critical change should not affect hash: %s vs %s", h1, h2)
	}
}

func TestCriticalFilesHash_CriticalChangeFlipsHash(t *testing.T) {
	cases := []struct {
		name string
		mut  fstest.MapFS
	}{
		{
			name: "Dockerfile",
			mut: fstest.MapFS{
				"Dockerfile":        {Data: []byte("FROM node:25\n")}, // changed
				"entrypoint.sh":     {Data: []byte("#!/bin/bash\n")},
				"lib/cloud-sync.sh": {Data: []byte("#!/bin/bash\n")},
			},
		},
		{
			name: "entrypoint.sh",
			mut: fstest.MapFS{
				"Dockerfile":        {Data: []byte("FROM node:24\n")},
				"entrypoint.sh":     {Data: []byte("#!/bin/bash\necho changed\n")}, // changed
				"lib/cloud-sync.sh": {Data: []byte("#!/bin/bash\n")},
			},
		},
		{
			name: "lib/*.sh",
			mut: fstest.MapFS{
				"Dockerfile":        {Data: []byte("FROM node:24\n")},
				"entrypoint.sh":     {Data: []byte("#!/bin/bash\n")},
				"lib/cloud-sync.sh": {Data: []byte("#!/bin/bash\necho NEW\n")}, // changed
			},
		},
	}

	base := fstest.MapFS{
		"Dockerfile":        {Data: []byte("FROM node:24\n")},
		"entrypoint.sh":     {Data: []byte("#!/bin/bash\n")},
		"lib/cloud-sync.sh": {Data: []byte("#!/bin/bash\n")},
	}
	baseHash, err := criticalFilesHash(base)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := criticalFilesHash(tc.mut)
			if err != nil {
				t.Fatal(err)
			}
			if h == baseHash {
				t.Fatalf("hash should change when %s changes", tc.name)
			}
		})
	}
}

func TestCriticalHashCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".payload-critical-hash")

	// Empty / missing file → empty string, no error.
	got, err := readCriticalHash(path)
	if err != nil {
		t.Fatalf("read missing: %v", err)
	}
	if got != "" {
		t.Errorf("read missing = %q, want empty", got)
	}

	if err := writeCriticalHash(path, "deadbeef"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err = readCriticalHash(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "deadbeef" {
		t.Errorf("round-trip = %q, want deadbeef", got)
	}
}

func TestDecideRebuild(t *testing.T) {
	tests := []struct {
		name      string
		extracted bool
		oldHash   string
		newHash   string
		wantBuild bool
		wantNo    bool
	}{
		{"not extracted", false, "abc", "abc", false, false},
		{"not extracted, hash mismatch ignored", false, "abc", "xyz", false, false},
		{"extracted, hash same → cached rebuild", true, "abc", "abc", true, false},
		{"extracted, hash changed → no-cache", true, "abc", "xyz", true, true},
		{"extracted, no prior hash, new value → no-cache", true, "", "xyz", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build, noCache := decideRebuild(tt.extracted, tt.oldHash, tt.newHash)
			if build != tt.wantBuild || noCache != tt.wantNo {
				t.Errorf("got (build=%v, noCache=%v), want (build=%v, noCache=%v)",
					build, noCache, tt.wantBuild, tt.wantNo)
			}
		})
	}
}

// --- Task B: per-namespace last-profile store -------------------------------

func TestLastProfileStore_GetMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-profile.json")

	store, err := loadLastProfileStore(path)
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if got := store.get("anything"); got.Profile != "" {
		t.Errorf("get on empty store = %q, want empty", got.Profile)
	}
}

func TestLastProfileStore_SaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-profile.json")

	if err := saveLastNamespace(path, "myrepo", "go", false); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := saveLastNamespace(path, "otherrepo", "frontend", true); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	// Overwrite an existing namespace.
	if err := saveLastNamespace(path, "myrepo", "infra", false); err != nil {
		t.Fatalf("save 3: %v", err)
	}

	store, err := loadLastProfileStore(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := store.get("myrepo"); got.Profile != "infra" {
		t.Errorf("myrepo = %q, want infra", got.Profile)
	}
	if got := store.get("otherrepo"); got.Profile != "frontend" || !got.Overview {
		t.Errorf("otherrepo = %+v, want {frontend true}", got)
	}
	if got := store.get("unknown"); got.Profile != "" {
		t.Errorf("unknown = %q, want empty", got.Profile)
	}

	// Sanity-check the on-disk shape.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Namespaces map[string]struct {
			Profile  string `json:"profile"`
			Overview bool   `json:"overview"`
		} `json:"namespaces"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Namespaces["myrepo"].Profile != "infra" {
		t.Errorf("on-disk myrepo = %q, want infra", parsed.Namespaces["myrepo"].Profile)
	}
}

func TestLastProfileStore_CorruptFileFallsBackToEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-profile.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := loadLastProfileStore(path)
	if err != nil {
		t.Fatalf("corrupt file should not error: %v", err)
	}
	if got := store.get("x"); got.Profile != "" {
		t.Errorf("corrupt → get = %q, want empty", got.Profile)
	}
}

func TestSelectProfileWithDefault_EOFUsesDefault(t *testing.T) {
	cfg := profilesConfig{
		Core: []string{"a"},
		Profiles: map[string]profileDef{
			"full":     {Description: "all", Servers: []string{}},
			"go":       {Description: "go", Servers: []string{}},
			"minimal":  {Description: "min", Servers: []string{}},
			"frontend": {Description: "fe", Servers: []string{}},
		},
	}

	// EOF on stdin: with a valid default, should pick the default rather
	// than hard-coded "full".
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close() // produce EOF
	defer r.Close()

	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	got, err := selectProfileWithDefault(cfg, "go")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != "go" {
		t.Errorf("default-on-EOF = %q, want go", got)
	}
}

func TestSelectProfileWithDefault_EmptyInputUsesDefault(t *testing.T) {
	cfg := profilesConfig{
		Core: []string{"a"},
		Profiles: map[string]profileDef{
			"full":    {Description: "all", Servers: []string{}},
			"go":      {Description: "go", Servers: []string{}},
			"minimal": {Description: "min", Servers: []string{}},
		},
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()
	defer r.Close()

	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	got, err := selectProfileWithDefault(cfg, "go")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != "go" {
		t.Errorf("default-on-blank = %q, want go", got)
	}
}

func TestSelectProfileWithDefault_UnknownDefaultFallsBackToFull(t *testing.T) {
	cfg := profilesConfig{
		Core: []string{"a"},
		Profiles: map[string]profileDef{
			"full":    {Description: "all", Servers: []string{}},
			"minimal": {Description: "min", Servers: []string{}},
		},
	}

	r, w, _ := os.Pipe()
	w.Close()
	defer r.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	got, err := selectProfileWithDefault(cfg, "nonexistent")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != "full" {
		t.Errorf("unknown default → %q, want full", got)
	}
}
