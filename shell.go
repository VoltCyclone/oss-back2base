package main

import (
	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Drop into a container shell",
	RunE:  runShell,
}

func init() {
	rootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	s, err := ensureReady()
	if err != nil {
		return err
	}

	if err := ensureBaseImage(resolveBaseImage()); err != nil {
		return err
	}

	var overrides []string
	if p := writeManagedSettingsOverride(s.cfg); p != "" {
		overrides = append(overrides, p)
	}
	composeArgs := baseComposeArgs(s.cfg, overrides...)
	composeArgs = append(composeArgs, "run", "--rm", "claude")

	return composeExec(composeArgs)
}
