package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastProfileStore_ReadsOldStringFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-profile.json")
	old := `{"namespaces":{"foo":"full","bar":"minor"}}`
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := loadLastProfileStore(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := store.get("foo"); got.Profile != "full" || got.Overview {
		t.Errorf("foo: got %+v, want {full false}", got)
	}
	if got := store.get("bar"); got.Profile != "minor" || got.Overview {
		t.Errorf("bar: got %+v, want {minor false}", got)
	}
}

func TestLastProfileStore_ReadsNewObjectFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-profile.json")
	newFmt := `{"namespaces":{"foo":{"profile":"full","overview":true}}}`
	if err := os.WriteFile(path, []byte(newFmt), 0644); err != nil {
		t.Fatal(err)
	}
	store, _ := loadLastProfileStore(path)
	got := store.get("foo")
	if got.Profile != "full" || !got.Overview {
		t.Errorf("got %+v, want {full true}", got)
	}
}

func TestSaveLastNamespace_PreservesOtherEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-profile.json")
	old := `{"namespaces":{"a":"full","b":"minor"}}`
	_ = os.WriteFile(path, []byte(old), 0644)

	if err := saveLastNamespace(path, "a", "research", true); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, _ := os.ReadFile(path)
	var parsed struct {
		Namespaces map[string]struct {
			Profile  string `json:"profile"`
			Overview bool   `json:"overview"`
		} `json:"namespaces"`
	}
	_ = json.Unmarshal(raw, &parsed)

	if parsed.Namespaces["a"].Profile != "research" || !parsed.Namespaces["a"].Overview {
		t.Errorf("a: got %+v", parsed.Namespaces["a"])
	}
	if parsed.Namespaces["b"].Profile != "minor" || parsed.Namespaces["b"].Overview {
		t.Errorf("b should migrate to {minor,false}: got %+v", parsed.Namespaces["b"])
	}
}

func TestLoadLastProfileStore_MissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := loadLastProfileStore(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if got := store.get("any"); got.Profile != "" || got.Overview {
		t.Errorf("missing ns should be zero entry, got %+v", got)
	}
}

func TestLastProfileStore_NullNamespacesMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-profile.json")
	if err := os.WriteFile(path, []byte(`{"namespaces":null}`), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := loadLastProfileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.get("x"); got.Profile != "" || got.Overview {
		t.Errorf("null namespaces should yield zero entry, got %+v", got)
	}
}

func TestSelectOverview_EmptyInputUsesDefault(t *testing.T) {
	for _, tc := range []struct {
		name  string
		def   bool
		input string
		want  bool
	}{
		{"default false, empty", false, "\n", false},
		{"default true, empty", true, "\n", true},
		{"default false, y", false, "y\n", true},
		{"default true, n", true, "n\n", false},
		{"default false, yes", false, "yes\n", true},
		{"default true, no", true, "no\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			got, err := selectOverviewWithDefault(strings.NewReader(tc.input), &stderr, tc.def)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelectOverview_EOFFallsBackToDefault(t *testing.T) {
	var stderr bytes.Buffer
	got, err := selectOverviewWithDefault(strings.NewReader(""), &stderr, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !got {
		t.Errorf("EOF with default=true should be true, got false")
	}
}

func TestSelectOverview_PromptShowsDefault(t *testing.T) {
	var stderr bytes.Buffer
	_, _ = selectOverviewWithDefault(strings.NewReader("\n"), &stderr, true)
	if !strings.Contains(stderr.String(), "[y/N, Enter for Y]") &&
		!strings.Contains(stderr.String(), "[Y/n, Enter for Y]") {
		t.Errorf("prompt should indicate default Y, got: %q", stderr.String())
	}
	stderr.Reset()
	_, _ = selectOverviewWithDefault(strings.NewReader("\n"), &stderr, false)
	if !strings.Contains(stderr.String(), "Enter for N") {
		t.Errorf("prompt should indicate default N, got: %q", stderr.String())
	}
}
