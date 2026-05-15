package main

import (
	"fmt"
	"os"
	"os/exec"
)

// baseImageTag may be injected via ldflags by downstream release builds. The
// OSS release pipeline leaves it as "dev" so resolveBaseImage falls back to the
// public :latest base image instead of requiring this repo to publish matching
// base-image tags.
var baseImageTag = "dev"

const baseImageRepo = "ramseymcgrath/back2base-base"

// resolveBaseImage returns the fully-qualified base image reference that the
// compose runtime should build on top of. Precedence:
//  1. BACK2BASE_BASE_IMAGE env var (explicit caller override — supports local
//     testing against a custom image).
//  2. The binary's embedded baseImageTag, when a downstream build chooses to
//     pin one.
//  3. :latest — OSS releases and local builds.
func resolveBaseImage() string {
	if v := os.Getenv("BACK2BASE_BASE_IMAGE"); v != "" {
		return v
	}
	if baseImageTag == "" || baseImageTag == "dev" {
		return baseImageRepo + ":latest"
	}
	return baseImageRepo + ":" + baseImageTag
}

// ensureBaseImage makes sure the given image exists in the local Docker
// daemon, pulling it if not. Silent on cache hit; on miss it streams the
// pull to stderr so the user sees why the first launch takes a minute.
//
// Called before compose build/run so first launch pulls the base image when the
// daemon has not already cached it.
func ensureBaseImage(image string) error {
	if err := exec.Command("docker", "image", "inspect", image).Run(); err == nil {
		return nil
	}
	fmt.Fprintf(os.Stderr, ":: Pulling %s\n", image)
	cmd := exec.Command("docker", "pull", image)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pull %s: %w", image, err)
	}
	return nil
}
