package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateJSONL_ValidLastRecord(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	writeFile(t, p, `{"a":1}`+"\n"+`{"b":2}`+"\n")
	if err := validateJSONL(p); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateJSONL_TrailingPartialLineOK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	// Last \n-terminated record is `{"ok":1}`. After it is a partial line.
	writeFile(t, p, `{"ok":1}`+"\n"+`{"part":`)
	if err := validateJSONL(p); err != nil {
		t.Fatalf("expected valid (trailing partial ignored), got %v", err)
	}
}

func TestValidateJSONL_EmptyFileInvalid(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	writeFile(t, p, "")
	if err := validateJSONL(p); err == nil {
		t.Fatal("expected invalid for empty file")
	}
}

func TestValidateJSONL_BadLastRecordInvalid(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	writeFile(t, p, `{"ok":1}`+"\n"+`{garbage}`+"\n")
	if err := validateJSONL(p); err == nil {
		t.Fatal("expected invalid for unparseable last record")
	}
}

func TestValidateJSONL_MissingFileInvalid(t *testing.T) {
	if err := validateJSONL(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Fatal("expected invalid for missing file")
	}
}

func TestListSessions_EmptyNamespaceDir(t *testing.T) {
	stateDir := t.TempDir()
	got, err := listSessions(stateDir, "neverexisted")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(got))
	}
}

func TestListSessions_SortsNewestFirst(t *testing.T) {
	stateDir := t.TempDir()
	ns := "ws"

	old := filepath.Join(stateDir, "projects", ns, "old", "old.jsonl")
	mid := filepath.Join(stateDir, "projects", ns, "mid", "mid.jsonl")
	new_ := filepath.Join(stateDir, "projects", ns, "new", "new.jsonl")
	for _, p := range []string{old, mid, new_} {
		writeFile(t, p, `{"ok":1}`+"\n")
	}

	now := time.Now()
	if err := os.Chtimes(old, now.Add(-3*time.Hour), now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(mid, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(new_, now, now); err != nil {
		t.Fatal(err)
	}

	got, err := listSessions(stateDir, ns)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(got))
	}
	if got[0].ID != "new" || got[1].ID != "mid" || got[2].ID != "old" {
		t.Fatalf("wrong order: %v %v %v", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestListSessions_SkipsDotDirs(t *testing.T) {
	stateDir := t.TempDir()
	ns := "ws"
	writeFile(t, filepath.Join(stateDir, "projects", ns, ".hidden", "x.jsonl"), `{"ok":1}`+"\n")
	writeFile(t, filepath.Join(stateDir, "projects", ns, "real", "x.jsonl"), `{"ok":1}`+"\n")
	got, err := listSessions(stateDir, ns)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "real" {
		t.Fatalf("expected only 'real', got %+v", got)
	}
}

func TestListSessions_SkipsSessionDirsWithNoJSONL(t *testing.T) {
	stateDir := t.TempDir()
	ns := "ws"
	if err := os.MkdirAll(filepath.Join(stateDir, "projects", ns, "blank"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stateDir, "projects", ns, "real", "x.jsonl"), `{"ok":1}`+"\n")
	got, err := listSessions(stateDir, ns)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "real" {
		t.Fatalf("expected only 'real', got %+v", got)
	}
}

func TestLatestSnapshot_PicksNewestValid(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, ".snapshots")
	a := filepath.Join(snap, "2026-01-01T00-00-00Z.jsonl")
	b := filepath.Join(snap, "2026-01-02T00-00-00Z.jsonl")
	writeFile(t, a, `{"ok":1}`+"\n")
	writeFile(t, b, `{"ok":2}`+"\n")
	now := time.Now()
	_ = os.Chtimes(a, now.Add(-1*time.Hour), now.Add(-1*time.Hour))
	_ = os.Chtimes(b, now, now)
	got, err := latestSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != b {
		t.Fatalf("expected %s, got %s", b, got)
	}
}

func TestLatestSnapshot_SkipsCorrupt(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, ".snapshots")
	good := filepath.Join(snap, "2026-01-01T00-00-00Z.jsonl")
	bad := filepath.Join(snap, "2026-01-02T00-00-00Z.jsonl")
	writeFile(t, good, `{"ok":1}`+"\n")
	writeFile(t, bad, `{not-json}`+"\n") // newer but corrupt
	now := time.Now()
	_ = os.Chtimes(good, now.Add(-1*time.Hour), now.Add(-1*time.Hour))
	_ = os.Chtimes(bad, now, now)
	got, err := latestSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != good {
		t.Fatalf("expected %s, got %s", good, got)
	}
}

func TestLatestSnapshot_NoSnapshotsErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := latestSnapshot(dir); err == nil {
		t.Fatal("expected error when .snapshots is missing")
	}
}

func TestLatestSnapshot_AllCorruptErrors(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, ".snapshots")
	writeFile(t, filepath.Join(snap, "a.jsonl"), `{bad}`+"\n")
	writeFile(t, filepath.Join(snap, "b.jsonl"), `{also-bad}`+"\n")
	if _, err := latestSnapshot(dir); err == nil {
		t.Fatal("expected error when no snapshot validates")
	}
}

func TestRestoreFromSnapshot_OverwritesLive(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live.jsonl")
	writeFile(t, live, `{"truncated":`)
	snap := filepath.Join(dir, ".snapshots", "snap.jsonl")
	writeFile(t, snap, `{"good":1}`+"\n")

	if err := restoreFromSnapshot(dir, snap); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"good":1}`+"\n" {
		t.Fatalf("live not restored: %q", got)
	}
}

func TestRestoreFromSnapshot_NoLiveErrors(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, ".snapshots", "s.jsonl")
	writeFile(t, snap, `{"a":1}`+"\n")
	if err := restoreFromSnapshot(dir, snap); err == nil {
		t.Fatal("expected error when no live jsonl exists")
	}
}

func TestRestoreFromSnapshot_LeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live.jsonl")
	writeFile(t, live, `{"old":1}`+"\n")
	snap := filepath.Join(dir, ".snapshots", "s.jsonl")
	writeFile(t, snap, `{"new":1}`+"\n")
	if err := restoreFromSnapshot(dir, snap); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func mkSession(t *testing.T, stateDir, ns, id, jsonl string, mt time.Time) string {
	t.Helper()
	p := filepath.Join(stateDir, "projects", ns, id, id+".jsonl")
	writeFile(t, p, jsonl)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(stateDir, "projects", ns, id)
}

func TestPickSession_NoSessionsErrors(t *testing.T) {
	_, err := pickSession(t.TempDir(), "ns", "")
	if !errors.Is(err, errNoSessions) {
		t.Fatalf("expected errNoSessions, got %v", err)
	}
}

func TestPickSession_NewestValid(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Now()
	mkSession(t, stateDir, "ws", "old", `{"ok":1}`+"\n", now.Add(-2*time.Hour))
	mkSession(t, stateDir, "ws", "new", `{"ok":2}`+"\n", now)
	got, err := pickSession(stateDir, "ws", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "new" {
		t.Fatalf("expected new, got %s", got.ID)
	}
}

func TestPickSession_SkipsCorruptToNextValid(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Now()
	mkSession(t, stateDir, "ws", "newcorrupt", `{garbage}`+"\n", now)
	mkSession(t, stateDir, "ws", "oldgood", `{"ok":1}`+"\n", now.Add(-1*time.Hour))
	got, err := pickSession(stateDir, "ws", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "oldgood" {
		t.Fatalf("expected oldgood, got %s", got.ID)
	}
}

func TestPickSession_FallsBackToSnapshotForCorruptLive(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Now()
	dir := mkSession(t, stateDir, "ws", "newcorrupt", `{garbage}`+"\n", now)
	writeFile(t, filepath.Join(dir, ".snapshots", "s.jsonl"), `{"ok":1}`+"\n")
	got, err := pickSession(stateDir, "ws", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "newcorrupt" {
		t.Fatalf("expected newcorrupt (restored from snapshot), got %s", got.ID)
	}
	if err := validateJSONL(got.LiveJSONL); err != nil {
		t.Fatalf("live should be restored: %v", err)
	}
}

func TestPickSession_AllCorruptNoSnapshotsErrors(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Now()
	mkSession(t, stateDir, "ws", "a", `{x}`+"\n", now)
	mkSession(t, stateDir, "ws", "b", `{y}`+"\n", now.Add(-1*time.Hour))
	_, err := pickSession(stateDir, "ws", "")
	if !errors.Is(err, errNoValidSession) {
		t.Fatalf("expected errNoValidSession, got %v", err)
	}
}

func TestPickSession_ExplicitIDWins(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Now()
	mkSession(t, stateDir, "ws", "older", `{"ok":1}`+"\n", now.Add(-3*time.Hour))
	mkSession(t, stateDir, "ws", "newer", `{"ok":2}`+"\n", now)
	got, err := pickSession(stateDir, "ws", "older")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "older" {
		t.Fatalf("expected older, got %s", got.ID)
	}
}

func TestPickSession_ExplicitIDUnknownErrors(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Now()
	mkSession(t, stateDir, "ws", "real", `{"ok":1}`+"\n", now)
	_, err := pickSession(stateDir, "ws", "ghost")
	var nf *errSessionNotFound
	if !errors.As(err, &nf) {
		t.Fatalf("expected errSessionNotFound, got %v", err)
	}
	if len(nf.Available) == 0 || nf.Available[0].ID != "real" {
		t.Fatalf("expected real in Available, got %+v", nf.Available)
	}
}
