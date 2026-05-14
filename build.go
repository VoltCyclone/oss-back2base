package main

import (
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the container image",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBuild(false)
	},
}

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Full rebuild (no cache)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBuild(true)
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(rebuildCmd)
}

func runBuild(noCache bool) error {
	s, err := ensureReady()
	if err != nil {
		return err
	}

	if err := ensureBaseImage(resolveBaseImage()); err != nil {
		return err
	}

	composeArgs := baseComposeArgs(s.cfg)
	composeArgs = append(composeArgs, "build")
	if noCache {
		// Full rebuilds also pull the base image; otherwise a stale
		// back2base-base in the local cache would silently keep old binaries
		// (e.g. memory-watcher) around. ensureBaseImage already pulled a
		// missing tag above; --pull here refreshes a stale same-tag digest.
		composeArgs = append(composeArgs, "--no-cache", "--pull")
	}

	return composeExec(composeArgs)
}
