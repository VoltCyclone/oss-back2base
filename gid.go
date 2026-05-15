package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var gidRegex = regexp.MustCompile(`^\d+$`)

func parseGIDOutput(output string) (string, bool) {
	s := strings.TrimSpace(output)
	if gidRegex.MatchString(s) {
		return s, true
	}
	return "", false
}

// probeGIDFunc is the function used to probe the docker socket GID.
// Replaceable in tests to avoid depending on Docker availability.
var probeGIDFunc = probeDockerGID

func probeDockerGID() string {
	cmd := exec.Command("docker", "run", "--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"busybox", "stat", "-c", "%g", "/var/run/docker.sock")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	gid, _ := parseGIDOutput(string(out))
	return gid
}

func resolveGID(envOverride, cacheFile string) string {
	if envOverride != "" {
		return envOverride
	}

	if probed := probeGIDFunc(); probed != "" {
		os.WriteFile(cacheFile, []byte(probed), 0644)
		return probed
	}

	if cached, err := os.ReadFile(cacheFile); err == nil {
		if gid, ok := parseGIDOutput(string(cached)); ok {
			return gid
		}
	}

	return "999"
}
