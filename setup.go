package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type setupResult struct {
	cfg       cbConfig
	gid       string
	extracted bool // true when this call re-wrote the embedded payload — signal to rebuild
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func ensureReady() (setupResult, error) {
	detectDockerHost()
	cfg := resolveConfig()

	if err := cfg.ensureDirs(); err != nil {
		return setupResult{}, err
	}

	hash := version + "-" + commit
	extracted, err := extractFS(shipFS(), cfg.Home, hash)
	if err != nil {
		return setupResult{}, fmt.Errorf("extract assets: %w", err)
	}

	seeded, _ := seedEnvFile(shipFS(), cfg.EnvFile)
	if seeded {
		fmt.Fprintf(os.Stderr, ":: Created %s — edit it to add your tokens.\n", cfg.EnvFile)
	}

	gidCache := filepath.Join(cfg.StateDir, "docker-gid")
	gid := resolveGID(os.Getenv("BACK2BASE_DOCKER_GID"), gidCache)

	os.Setenv("BACK2BASE_HOME", cfg.Home)
	os.Setenv("BACK2BASE_CONFIG", cfg.ConfigDir)
	os.Setenv("BACK2BASE_STATE", cfg.StateDir)
	os.Setenv("REPO_PATH", mustGetwd())
	os.Setenv("BACK2BASE_DOCKER_GID", gid)

	return setupResult{cfg: cfg, gid: gid, extracted: extracted}, nil
}

// detectDockerHost sets DOCKER_HOST if not already set, probing common
// socket paths for Colima, Docker Desktop, and native Linux. This matches
// the detect_host_docker_sock() function from the original install.sh.
func detectDockerHost() {
	if dh := os.Getenv("DOCKER_HOST"); dh != "" {
		// Respect existing DOCKER_HOST (whether set by user or runtime)
		if strings.HasPrefix(dh, "unix://") {
			return
		}
	}

	home := os.Getenv("HOME")
	candidates := []string{
		filepath.Join(home, ".colima", "default", "docker.sock"),
		filepath.Join(home, ".docker", "run", "docker.sock"),
		"/var/run/docker.sock",
	}

	for _, sock := range candidates {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			os.Setenv("DOCKER_HOST", "unix://"+sock)
			return
		}
	}
}
