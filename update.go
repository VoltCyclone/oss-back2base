package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update oss-back2base to the latest version",
	RunE:  runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	if isHomebrewInstall() {
		fmt.Println("oss-back2base appears to be installed via Homebrew. Run:")
		fmt.Println("  brew upgrade oss-back2base")
		return nil
	}

	if isAptInstall() {
		fmt.Println("oss-back2base appears to be installed via apt. Run:")
		fmt.Println("  sudo apt update && sudo apt upgrade oss-back2base")
		return nil
	}

	fmt.Printf("oss-back2base %s — checking for updates...\n", version)

	newVer, url, err := checkForUpdate()
	if err != nil {
		return fmt.Errorf("update check: %w", err)
	}
	if newVer == "" {
		fmt.Println("Already up to date.")
		return nil
	}

	fmt.Printf("Updating to v%s...\n", newVer)
	if err := doSelfUpdate(url); err != nil {
		return err
	}

	fmt.Printf("Updated to v%s. Run 'oss-back2base version' to confirm.\n", newVer)

	// Auto-prune via the just-installed binary so its embedded baseImageTag
	// (the new version) is the "current" pin. Best-effort: any failure is
	// silent — the binary swap already succeeded.
	if exe, err := os.Executable(); err == nil {
		_ = exec.Command(exe, "prune", "--yes", "--quiet").Run()
	}
	return nil
}
