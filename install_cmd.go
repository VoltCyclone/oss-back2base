package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Run first-time setup (check prereqs, build image)",
	RunE:  runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	cfg := resolveConfig()

	fmt.Println()
	fmt.Println("  back2base — containerized Claude Code")
	fmt.Println("  ─────────────────────────────────────")
	fmt.Println()
	fmt.Println("  Install paths:")
	fmt.Printf("    BACK2BASE_HOME   = %s\n", cfg.Home)
	fmt.Printf("    BACK2BASE_CONFIG = %s\n", cfg.ConfigDir)
	fmt.Printf("    Binary           = %s\n\n", execPath())

	// Step 1: Check prerequisites
	fmt.Println("[1/4] Checking prerequisites")
	if err := checkPrereqs(); err != nil {
		return err
	}

	// Step 2: Extract assets
	fmt.Printf("\n[2/4] Extracting assets to %s\n", cfg.Home)
	if err := cfg.ensureDirs(); err != nil {
		return err
	}
	// Use the same hash format as ensureReady() so the next `back2base` run
	// doesn't re-extract. Force extraction by removing the hash file first.
	hash := version + "-" + commit
	os.Remove(filepath.Join(cfg.Home, ".extract-hash"))
	if _, err := extractFS(shipFS(), cfg.Home, hash); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	fmt.Println(":: Assets extracted")

	// Step 3: Seed config
	fmt.Printf("\n[3/4] Setting up %s\n", cfg.ConfigDir)
	seeded, err := seedEnvFile(shipFS(), cfg.EnvFile)
	if err != nil {
		return err
	}
	if seeded {
		fmt.Printf(":: Seeded %s from template\n", cfg.EnvFile)
		fmt.Println(":: Edit it to add your tokens before running back2base.")
	} else {
		fmt.Printf(":: Existing env file preserved at %s\n", cfg.EnvFile)
	}

	// Step 4: Build image
	fmt.Println("\n[4/4] Building container image")
	fmt.Println()
	fmt.Println(":: This may take a few minutes on first run...")
	fmt.Println()

	gidCache := filepath.Join(cfg.StateDir, "docker-gid")
	gid := resolveGID(os.Getenv("BACK2BASE_DOCKER_GID"), gidCache)

	os.Setenv("BACK2BASE_HOME", cfg.Home)
	os.Setenv("BACK2BASE_CONFIG", cfg.ConfigDir)
	os.Setenv("BACK2BASE_STATE", cfg.StateDir)
	os.Setenv("REPO_PATH", mustGetwd())
	os.Setenv("BACK2BASE_DOCKER_GID", gid)

	if err := ensureBaseImage(resolveBaseImage()); err != nil {
		return err
	}

	composeArgs := baseComposeArgs(cfg)
	composeArgs = append(composeArgs, "build")
	if err := composeRun(composeArgs); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	fmt.Println()
	fmt.Println("  Setup complete!")
	fmt.Println()
	fmt.Println("  Get started:")
	fmt.Println()
	fmt.Println("    back2base login                    Authenticate (first time)")
	fmt.Println("    back2base                          Launch Claude Code")
	fmt.Printf("    back2base -p \"...\"                 One-shot prompt\n")
	fmt.Println()
	fmt.Printf("  Edit config: %s\n\n", cfg.EnvFile)

	return nil
}

func checkPrereqs() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH")
	}

	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		return fmt.Errorf("docker compose not available")
	}

	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("Docker is installed but not running. Start Docker Desktop and try again")
	}

	dockerOut, _ := exec.Command("docker", "--version").Output()
	fmt.Printf(":: docker %s", dockerOut)
	fmt.Println(":: docker compose available")

	return nil
}
