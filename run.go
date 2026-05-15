package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.RunE = runClaude
}

func runClaude(cmd *cobra.Command, args []string) error {
	s, err := ensureReady()
	if err != nil {
		return err
	}

	// Resolve REPO_PATH. If we're inside a git tree, walk up to the repo
	// root so subdir launches don't fragment namespaces (launching in
	// `myapp/cache/` used to produce namespace=`cache`). Priority:
	// --repo flag > existing env > git-root-from-cwd > cwd.
	repoPath := flagRepo
	if repoPath == "" {
		repoPath = os.Getenv("REPO_PATH")
	}
	if repoPath == "" {
		if wd, err := os.Getwd(); err == nil {
			if root := gitRoot(wd); root != "" {
				repoPath = root
			} else {
				repoPath = wd
			}
		}
	}
	if repoPath != "" {
		if abs, err := filepath.Abs(repoPath); err == nil {
			repoPath = abs
		}
		os.Setenv("REPO_PATH", repoPath)
		ns := resolveNamespace(repoPath)
		os.Setenv("WORKSPACE_NAME", ns)
		os.Setenv("MEMORY_NAMESPACE", ns)
		fmt.Fprintf(os.Stderr, ":: memory namespace: %s\n", ns)
	}

	// Resolve extra dirs to absolute paths
	absDirs := make([]string, 0, len(flagDirs))
	for _, d := range flagDirs {
		if abs, err := filepath.Abs(d); err == nil {
			absDirs = append(absDirs, abs)
		} else {
			absDirs = append(absDirs, d)
		}
	}

	prompt := flagPrompt
	if prompt == "" && len(args) > 0 {
		prompt = strings.Join(args, " ")
	}

	// Pull the per-namespace last-selected profile so the interactive
	// selector can pre-select it and `--profile=last` can resolve to it.
	ns := os.Getenv("MEMORY_NAMESPACE")
	lastProfilePath := filepath.Join(s.cfg.StateDir, "last-profile.json")
	lastStore, _ := loadLastProfileStore(lastProfilePath)
	var remembered lastNamespaceEntry
	if ns != "" {
		remembered = lastStore.get(ns)
	}

	// Resolve profile: flag > env > interactive selector (skip for one-shot prompts).
	// `--profile=last` (or BACK2BASE_PROFILE=last) resolves to the remembered
	// value for this namespace, falling back to "full" if nothing was saved.
	profile := flagProfile
	if profile == "" {
		profile = os.Getenv("BACK2BASE_PROFILE")
	}
	if profile == "last" {
		if remembered.Profile != "" {
			profile = remembered.Profile
		} else {
			profile = "full"
		}
	}
	if profile == "" {
		if prompt != "" {
			// One-shot mode: default to remembered profile, else full,
			// without showing the interactive prompt.
			if remembered.Profile != "" {
				profile = remembered.Profile
			} else {
				profile = "full"
			}
		} else {
			// Interactive mode: show selector, pre-selecting the
			// remembered profile when one exists.
			profiles, err := loadProfiles(shipFS())
			if err != nil {
				return fmt.Errorf("load profiles: %w", err)
			}
			profile, err = selectProfileWithDefault(profiles, remembered.Profile)
			if err != nil {
				return err
			}
		}
	}
	os.Setenv("BACK2BASE_PROFILE", profile)
	fmt.Fprintf(os.Stderr, ":: Profile: %s\n", profile)

	// Resolve overview: --no-overview > --overview > env > prompt > remembered > false.
	// One-shot mode (prompt non-empty) skips the interactive prompt and falls back
	// to remembered/false without asking, matching profile resolution behavior.
	overview := remembered.Overview
	overviewSet := false
	switch {
	case flagNoOverview:
		overview = false
		overviewSet = true
	case flagOverview:
		overview = true
		overviewSet = true
	}
	if !overviewSet {
		if envVal := os.Getenv("BACK2BASE_OVERVIEW"); envVal != "" {
			overview = envVal == "1" || strings.EqualFold(envVal, "true") || strings.EqualFold(envVal, "yes")
			overviewSet = true
		}
	}
	if !overviewSet && prompt == "" {
		// Interactive mode: ask, pre-selecting the remembered choice.
		picked, err := selectOverviewWithDefault(os.Stdin, os.Stderr, remembered.Overview)
		if err != nil {
			return err
		}
		overview = picked
	}
	if overview {
		os.Setenv("BACK2BASE_OVERVIEW", "1")
		fmt.Fprintln(os.Stderr, ":: Overview: enabled")
	} else {
		os.Setenv("BACK2BASE_OVERVIEW", "0")
	}

	// Pin the user's intent for next launch in this namespace. Best-effort:
	// failure to persist shouldn't block the run.
	if ns != "" && profile != "" {
		if err := saveLastNamespace(lastProfilePath, ns, profile, overview); err != nil {
			fmt.Fprintf(os.Stderr, ":: warning: could not save last profile: %v\n", err)
		}
	}

	// Model resolution: explicit BACK2BASE_MODEL wins; otherwise fall back to
	// the profile's pinned model (non-minor profiles pin claude-opus-4-7[1m]).
	model := os.Getenv("BACK2BASE_MODEL")
	if model == "" {
		if profiles, err := loadProfiles(shipFS()); err == nil {
			if p, ok := profiles.Profiles[profile]; ok && p.Model != "" {
				model = p.Model
				os.Setenv("BACK2BASE_MODEL", model)
			}
		}
	}
	if model != "" {
		fmt.Fprintf(os.Stderr, ":: Model: %s\n", model)
	}

	if err := ensureBaseImage(resolveBaseImage()); err != nil {
		return err
	}

	// Decide whether to rebuild. Cheap default: docker's COPY-layer cache
	// already detects content changes in defaults/, lib/, commands/, skills/.
	// We only need --no-cache --pull when a "critical" file actually changed
	// — Dockerfile, top-level *.sh (entrypoint, init-firewall), or lib/*.sh
	// (shell helpers shipped into the image). Anything else: a normal `build`
	// is enough.
	criticalHashPath := filepath.Join(s.cfg.Home, ".payload-critical-hash")
	newHash, hashErr := criticalFilesHash(shipFS())
	if hashErr != nil {
		// If we can't hash, fall back to the conservative behavior:
		// when payload was extracted, do a no-cache rebuild.
		if s.extracted {
			fmt.Fprintf(os.Stderr, ":: warning: hashing critical files failed (%v) — falling back to --no-cache --pull\n", hashErr)
			if err := runBuild(true); err != nil {
				return err
			}
		}
	} else {
		oldHash, _ := readCriticalHash(criticalHashPath)
		rebuild, noCache := decideRebuild(s.extracted, oldHash, newHash)
		if rebuild {
			if noCache {
				fmt.Fprintln(os.Stderr, ":: critical container files changed — rebuilding image (--no-cache --pull)")
			} else {
				fmt.Fprintln(os.Stderr, ":: container payload updated — rebuilding image (cached layers reused)")
			}
			if err := runBuild(noCache); err != nil {
				return err
			}
			// Only persist the new hash after a successful no-cache rebuild
			// — otherwise a half-finished upgrade could leave the cache
			// claiming "all current" when the image hasn't actually been
			// rebuilt with the new critical files.
			if noCache {
				if err := writeCriticalHash(criticalHashPath, newHash); err != nil {
					fmt.Fprintf(os.Stderr, ":: warning: could not persist critical hash: %v\n", err)
				}
			}
		}
	}

	composeArgs := buildRunArgs(s.cfg, runOpts{
		extraDirs: absDirs,
		prompt:    prompt,
		model:     model,
	})

	return composeExec(composeArgs)
}

// resolveNamespace picks a stable per-project identity for memory storage.
// Resolution order, first non-empty wins:
//
//  1. --namespace flag
//  2. MEMORY_NAMESPACE env
//  3. `git config --get remote.origin.url` last segment (git repo with remote)
//  4. .back2base/namespace file (non-git dirs only — never auto-written
//     inside a git repo, by design; we don't touch the user's tracked tree)
//  5. basename(repoPath) — final fallback. Written to .back2base/namespace
//     in non-git dirs so future launches pin.
func resolveNamespace(repoPath string) string {
	if flagNamespace != "" {
		return flagNamespace
	}
	if env := os.Getenv("MEMORY_NAMESPACE"); env != "" {
		return env
	}
	if inGit := gitRoot(repoPath) != ""; inGit {
		if name := gitRemoteNamespace(repoPath); name != "" {
			return name
		}
		// git repo with no remote — use basename, don't write anything
		return filepath.Base(repoPath)
	}
	// Non-git: honor pinned .back2base/namespace, else create one.
	pinPath := filepath.Join(repoPath, ".back2base", "namespace")
	if b, err := os.ReadFile(pinPath); err == nil {
		if pinned := strings.TrimSpace(string(b)); pinned != "" {
			return pinned
		}
	}
	ns := filepath.Base(repoPath)
	_ = os.MkdirAll(filepath.Dir(pinPath), 0755)
	_ = os.WriteFile(pinPath, []byte(ns+"\n"), 0644)
	fmt.Fprintf(os.Stderr, ":: pinned memory namespace: %s → %s\n", ns, pinPath)
	return ns
}

// gitRoot returns the repo root containing dir, or "" if not in a git tree.
func gitRoot(dir string) string {
	if dir == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// criticalFilesHash computes a sha256 over the files whose contents directly
// invalidate docker's build cache: the Dockerfile, top-level shell scripts
// (entrypoint.sh, init-firewall.sh) and shell helpers under lib/. Edits to
// these mean a `RUN`-step or COPY target changed; everything else (defaults/,
// commands/, skills/) is content COPY'd in by the Dockerfile and is already
// invalidated layer-by-layer by docker's own content hashing.
//
// The hash is deterministic: paths are sorted, and for each entry we mix in
// "<path>\n<len>\n<content>\n" so two files can't collide by concatenation.
func criticalFilesHash(assets fs.FS) (string, error) {
	var paths []string
	walkErr := fs.WalkDir(assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isCriticalContainerFile(path) {
			paths = append(paths, path)
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		data, err := fs.ReadFile(assets, p)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", p, err)
		}
		fmt.Fprintf(h, "%s\n%d\n", p, len(data))
		h.Write(data)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isCriticalContainerFile decides whether a path inside shipFS() is one
// whose change should trigger a `--no-cache --pull` rebuild.
func isCriticalContainerFile(path string) bool {
	// shipFS() is rooted at back2base-container/, so paths look like
	// "Dockerfile", "entrypoint.sh", "lib/session-snapshot.sh", "defaults/...".
	if path == "Dockerfile" {
		return true
	}
	dir, file := filepath.Split(path)
	dir = strings.TrimSuffix(dir, "/")
	if !strings.HasSuffix(file, ".sh") {
		return false
	}
	if dir == "" {
		// top-level *.sh (entrypoint.sh, init-firewall.sh)
		return true
	}
	if dir == "lib" {
		return true
	}
	return false
}

// readCriticalHash reads the cached critical-files hash. A missing file is
// not an error — first runs return ("", nil).
func readCriticalHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// writeCriticalHash atomically replaces the cached hash on disk.
func writeCriticalHash(path, hash string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(hash), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// decideRebuild returns (rebuild, noCache) for the run-time payload
// freshness decision. Pure function so it's straightforward to test:
//   - extracted=false: nothing changed since the last launch — skip build.
//   - extracted=true, hash unchanged: only non-critical container assets
//     moved (e.g. defaults/, commands/, skills/). A normal cached build
//     lets COPY-layer hashing do the right thing.
//   - extracted=true, hash changed: a critical file (Dockerfile, top-level
//     *.sh, lib/*.sh) was edited — force --no-cache --pull.
func decideRebuild(extracted bool, oldHash, newHash string) (bool, bool) {
	if !extracted {
		return false, false
	}
	if oldHash == newHash {
		return true, false
	}
	return true, true
}

// --- Per-namespace last-profile memory ----------------------------------------

// lastNamespaceEntry is the new per-namespace record. Old files stored a
// plain string (the profile name); UnmarshalJSON below migrates those into
// {Profile: <string>, Overview: false} on read so older state survives an
// upgrade in place.
type lastNamespaceEntry struct {
	Profile  string `json:"profile"`
	Overview bool   `json:"overview"`
}

type lastProfileStore struct {
	Namespaces map[string]lastNamespaceEntry `json:"namespaces"`
}

// UnmarshalJSON accepts either the new object form (lastNamespaceEntry) or
// the legacy plain-string form (just the profile name) per namespace value.
// Unknown / unparseable forms are dropped silently — same graceful-degrade
// posture as the original parser.
func (s *lastProfileStore) UnmarshalJSON(data []byte) error {
	var raw struct {
		Namespaces map[string]json.RawMessage `json:"namespaces"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Namespaces = make(map[string]lastNamespaceEntry, len(raw.Namespaces))
	for k, v := range raw.Namespaces {
		var asString string
		if err := json.Unmarshal(v, &asString); err == nil {
			s.Namespaces[k] = lastNamespaceEntry{Profile: asString}
			continue
		}
		var asEntry lastNamespaceEntry
		if err := json.Unmarshal(v, &asEntry); err == nil {
			s.Namespaces[k] = asEntry
			continue
		}
	}
	return nil
}

// get returns the saved entry for a namespace, or a zero entry if none.
func (s lastProfileStore) get(namespace string) lastNamespaceEntry {
	return s.Namespaces[namespace]
}

// loadLastProfileStore reads the on-disk JSON store. A missing or corrupt
// file is treated as "no memory yet" — the caller can keep going with an
// empty store.
func loadLastProfileStore(path string) (lastProfileStore, error) {
	store := lastProfileStore{Namespaces: map[string]lastNamespaceEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return store, err
	}
	var parsed lastProfileStore
	if err := json.Unmarshal(data, &parsed); err != nil {
		// Don't surface the parse error to callers — the failure mode is
		// "user dropped a typo into the file" or "we changed the schema",
		// neither of which should brick the launcher.
		return store, nil
	}
	return parsed, nil
}

// saveLastNamespace merges (namespace → {profile, overview}) into the store
// and rewrites the JSON file atomically. Replaces the old saveLastProfile.
func saveLastNamespace(path, namespace, profile string, overview bool) error {
	store, _ := loadLastProfileStore(path)
	store.Namespaces[namespace] = lastNamespaceEntry{Profile: profile, Overview: overview}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// gitRemoteNamespace returns the last path segment of the origin remote
// URL, or "" if no remote is configured.
func gitRemoteNamespace(repoPath string) string {
	out, err := exec.Command("git", "-C", repoPath, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return ""
	}
	remote := strings.TrimSpace(string(out))
	remote = strings.TrimSuffix(remote, "/")
	remote = strings.TrimSuffix(remote, ".git")
	if idx := strings.LastIndexAny(remote, "/:"); idx >= 0 {
		remote = remote[idx+1:]
	}
	return remote
}
