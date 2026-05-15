package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// validateJSONL returns nil iff path exists, is non-empty, and the last
// \n-terminated record parses as JSON. A trailing partial line (no
// terminating \n) is allowed — claude often has one in flight.
func validateJSONL(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("empty file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Assumes no single JSONL record exceeds tailWindow. 64 KiB is much
	// larger than Claude Code's per-message records in practice.
	const tailWindow = 64 * 1024
	start := info.Size() - tailWindow
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	lastNL := bytes.LastIndexByte(buf, '\n')
	if lastNL < 0 {
		return fmt.Errorf("no terminated record in tail: %s", path)
	}
	recStart := bytes.LastIndexByte(buf[:lastNL], '\n') + 1
	record := bytes.TrimSpace(buf[recStart:lastNL])
	if len(record) == 0 {
		return fmt.Errorf("empty last record: %s", path)
	}
	var v any
	if err := json.Unmarshal(record, &v); err != nil {
		return fmt.Errorf("invalid JSON in last record of %s: %w", path, err)
	}
	return nil
}

type sessionInfo struct {
	ID        string    // UUID directory name
	Dir       string    // absolute path to session dir
	LiveJSONL string    // absolute path to newest *.jsonl in Dir
	LastWrite time.Time // mtime of LiveJSONL
}

// listSessions returns all session dirs under <stateDir>/projects/<ns>/,
// sorted newest-first by the mtime of each session's newest *.jsonl.
// Sessions with no .jsonl are skipped. Hidden (dot-prefixed) entries are
// skipped. Missing namespace dir returns (nil, nil).
func listSessions(stateDir, ns string) ([]sessionInfo, error) {
	nsDir := filepath.Join(stateDir, "projects", ns)
	entries, err := os.ReadDir(nsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sessions []sessionInfo
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(nsDir, e.Name())
		live, mt, err := newestTopLevelJSONL(dir)
		if err != nil || live == "" {
			continue
		}
		sessions = append(sessions, sessionInfo{
			ID: e.Name(), Dir: dir, LiveJSONL: live, LastWrite: mt,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastWrite.After(sessions[j].LastWrite)
	})
	return sessions, nil
}

// newestTopLevelJSONL returns the path + mtime of the newest *.jsonl
// directly under dir (does not descend into subdirectories — so .snapshots
// content is intentionally excluded).
func newestTopLevelJSONL(dir string) (string, time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, err
	}
	var (
		bestPath string
		bestMt   time.Time
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestMt) {
			bestMt = info.ModTime()
			bestPath = filepath.Join(dir, e.Name())
		}
	}
	return bestPath, bestMt, nil
}

func latestSnapshot(sessionDir string) (string, error) {
	snapDir := filepath.Join(sessionDir, ".snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		return "", err
	}
	type cand struct {
		path string
		mt   time.Time
	}
	var cands []cand
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cands = append(cands, cand{filepath.Join(snapDir, e.Name()), info.ModTime()})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mt.After(cands[j].mt) })
	for _, c := range cands {
		if validateJSONL(c.path) == nil {
			return c.path, nil
		}
	}
	return "", fmt.Errorf("no valid snapshot in %s", snapDir)
}

// restoreFromSnapshot writes the contents of snapPath over the session's
// live JSONL using a tmp+rename so readers never see a partial file. The
// "live JSONL" is the newest top-level *.jsonl in sessionDir.
func restoreFromSnapshot(sessionDir, snapPath string) error {
	livePath, _, err := newestTopLevelJSONL(sessionDir)
	if err != nil {
		return err
	}
	if livePath == "" {
		return fmt.Errorf("no live jsonl in %s", sessionDir)
	}
	src, err := os.Open(snapPath)
	if err != nil {
		return err
	}
	defer src.Close()
	tmpPath := livePath + ".restore.tmp"
	dst, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, livePath)
}

var (
	errNoSessions     = errors.New("no prior sessions for namespace")
	errNoValidSession = errors.New("no resumable session (all live JSONLs corrupt and no valid snapshots)")
)

type errSessionNotFound struct {
	ID        string
	Available []sessionInfo
}

func (e *errSessionNotFound) Error() string {
	return fmt.Sprintf("session %s not found", e.ID)
}

// pickSession selects a session for resume. If idArg is non-empty, only that
// session is considered; otherwise sessions are tried newest-first. For each
// candidate, the live JSONL is validated; on validation failure we attempt to
// restore from the latest valid snapshot before accepting.
func pickSession(stateDir, ns, idArg string) (sessionInfo, error) {
	sessions, err := listSessions(stateDir, ns)
	if err != nil {
		return sessionInfo{}, err
	}
	if len(sessions) == 0 {
		return sessionInfo{}, errNoSessions
	}

	tryRecover := func(s sessionInfo) (sessionInfo, error) {
		if vErr := validateJSONL(s.LiveJSONL); vErr == nil {
			return s, nil
		}
		snap, sErr := latestSnapshot(s.Dir)
		if sErr != nil {
			return sessionInfo{}, sErr
		}
		if rErr := restoreFromSnapshot(s.Dir, snap); rErr != nil {
			return sessionInfo{}, rErr
		}
		return s, nil
	}

	if idArg != "" {
		for _, s := range sessions {
			if s.ID == idArg {
				return tryRecover(s)
			}
		}
		return sessionInfo{}, &errSessionNotFound{ID: idArg, Available: sessions}
	}

	for _, s := range sessions {
		if got, err := tryRecover(s); err == nil {
			return got, nil
		}
	}
	return sessionInfo{}, errNoValidSession
}
