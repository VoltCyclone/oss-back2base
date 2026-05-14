// resume.go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [sessionId]",
	Short: "Resume the most recent (or named) Claude Code session in this workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runResume,
}

func init() {
	rootCmd.AddCommand(resumeCmd)
}

func runResume(cmd *cobra.Command, args []string) error {
	s, err := ensureReady()
	if err != nil {
		return err
	}

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
	}
	ns := resolveNamespace(repoPath)
	os.Setenv("WORKSPACE_NAME", ns)
	os.Setenv("MEMORY_NAMESPACE", ns)
	fmt.Fprintf(os.Stderr, ":: memory namespace: %s\n", ns)

	idArg := ""
	if len(args) == 1 {
		idArg = args[0]
	}

	picked, err := pickSession(s.cfg.StateDir, ns, idArg)
	if err != nil {
		return formatPickError(err, ns)
	}

	age := time.Since(picked.LastWrite).Round(time.Second)
	fmt.Fprintf(os.Stderr, ":: resuming session %s (last write %s ago)\n", picked.ID, age)

	if err := ensureBaseImage(resolveBaseImage()); err != nil {
		return err
	}
	if s.extracted {
		fmt.Fprintln(os.Stderr, ":: container payload updated — rebuilding image (--no-cache --pull)")
		if err := runBuild(true); err != nil {
			return err
		}
	}

	composeArgs := buildRunArgs(s.cfg, runOpts{
		resumeID: picked.ID,
	})
	return composeExec(composeArgs)
}

func formatPickError(err error, ns string) error {
	if errors.Is(err, errNoSessions) {
		return fmt.Errorf("no prior sessions for namespace %q; run `back2base run` to start fresh", ns)
	}
	if errors.Is(err, errNoValidSession) {
		return fmt.Errorf("no resumable session for namespace %q (all live JSONLs corrupt and no valid snapshots); run `back2base run` to start fresh", ns)
	}
	var nf *errSessionNotFound
	if errors.As(err, &nf) {
		ids := make([]string, 0, len(nf.Available))
		for i, s := range nf.Available {
			if i >= 5 {
				break
			}
			ids = append(ids, s.ID)
		}
		return fmt.Errorf("session %q not found; recent sessions: %v", nf.ID, ids)
	}
	return err
}
