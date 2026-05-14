package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
)

type profileDef struct {
	Description string   `json:"description"`
	Model       string   `json:"model,omitempty"`
	Servers     []string `json:"servers"`
}

type profilesConfig struct {
	Core     []string              `json:"core"`
	Profiles map[string]profileDef `json:"profiles"`
}

func loadProfiles(assets fs.FS) (profilesConfig, error) {
	data, err := fs.ReadFile(assets, "defaults/profiles.json")
	if err != nil {
		return profilesConfig{}, fmt.Errorf("read profiles.json: %w", err)
	}
	var cfg profilesConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return profilesConfig{}, fmt.Errorf("parse profiles.json: %w", err)
	}
	return cfg, nil
}

// resolvedServers returns the full server list for a profile (core + profile-specific).
func (c profilesConfig) resolvedServers(name string) ([]string, error) {
	p, ok := c.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("unknown profile: %q", name)
	}
	seen := make(map[string]bool)
	var servers []string
	for _, s := range c.Core {
		if !seen[s] {
			seen[s] = true
			servers = append(servers, s)
		}
	}
	for _, s := range p.Servers {
		if !seen[s] {
			seen[s] = true
			servers = append(servers, s)
		}
	}
	sort.Strings(servers)
	return servers, nil
}

// profileNames returns sorted profile names for display order.
// "full" is always first, "minimal" always last, rest alphabetical.
func (c profilesConfig) profileNames() []string {
	var names []string
	for k := range c.Profiles {
		if k != "full" && k != "minimal" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	result := []string{"full"}
	result = append(result, names...)
	result = append(result, "minimal")
	return result
}

// selectProfile shows an interactive numbered menu on stderr and reads
// the user's choice from stdin. Returns the selected profile name.
func selectProfile(cfg profilesConfig) (string, error) {
	return selectProfileWithDefault(cfg, "")
}

// selectProfileWithDefault is selectProfile with a caller-supplied
// "remembered" profile. An empty or unknown defaultName falls back to
// "full" (the original behavior). The default is the value used on EOF
// or empty-line input.
func selectProfileWithDefault(cfg profilesConfig, defaultName string) (string, error) {
	names := cfg.profileNames()

	chosenDefault := "full"
	if defaultName != "" {
		if _, ok := cfg.Profiles[defaultName]; ok {
			chosenDefault = defaultName
		}
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Select a tool profile:")
	fmt.Fprintln(os.Stderr)
	for i, name := range names {
		p := cfg.Profiles[name]
		servers, _ := cfg.resolvedServers(name)
		marker := " "
		if name == chosenDefault {
			marker = "*"
		}
		fmt.Fprintf(os.Stderr, " %s %d) %-12s %s (%d servers)\n", marker, i+1, name, p.Description, len(servers))
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Profile [1-%s, name, or Enter for %s]: ", strconv.Itoa(len(names)), chosenDefault)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return chosenDefault, nil
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return chosenDefault, nil
	}

	// Try as number first
	if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= len(names) {
		return names[n-1], nil
	}

	// Try as name
	if _, ok := cfg.Profiles[input]; ok {
		return input, nil
	}

	return "", fmt.Errorf("invalid profile: %q", input)
}
