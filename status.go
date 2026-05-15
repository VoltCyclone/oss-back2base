package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show container/volume status",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg := resolveConfig()

	fmt.Println()
	fmt.Println("Install paths:")
	fmt.Printf("  OSS_BACK2BASE_HOME   = %s\n", cfg.Home)
	fmt.Printf("  OSS_BACK2BASE_CONFIG = %s\n", cfg.ConfigDir)
	fmt.Printf("  Binary               = %s (v%s)\n", execPath(), version)
	fmt.Println()

	fmt.Println("Env file:")
	if _, err := os.Stat(cfg.EnvFile); err == nil {
		fmt.Printf("  %s (present)\n", cfg.EnvFile)
	} else {
		fmt.Printf("  (no env file at %s)\n", cfg.EnvFile)
	}
	fmt.Println()

	// Images
	fmt.Println("Image:")
	out, err := exec.Command("docker", "images",
		"--format", "  {{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}").Output()
	if err == nil {
		found := false
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "claude") {
				fmt.Println(line)
				found = true
			}
		}
		if !found {
			fmt.Println("  (not built)")
		}
	} else {
		fmt.Println("  (docker not available)")
	}
	fmt.Println()

	// Containers
	fmt.Println("Running containers:")
	out, err = exec.Command("docker", "ps",
		"--format", "  {{.Names}}\t{{.Status}}").Output()
	if err == nil {
		found := false
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "claude") {
				fmt.Println(line)
				found = true
			}
		}
		if !found {
			fmt.Println("  (none)")
		}
	}
	fmt.Println()

	return nil
}

func execPath() string {
	p, err := os.Executable()
	if err != nil {
		return "(unknown)"
	}
	return p
}
