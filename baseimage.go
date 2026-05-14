package main

import (
	"fmt"
	"os"
	"os/exec"
)

// baseImageTag is injected via ldflags at release time (Makefile and
// .goreleaser.yml wire it to the same string as main.version, which for
// goreleaser is `v<semver>`). When built locally without LDFLAGS it stays
// "dev", which resolveBaseImage treats as "no pin" and falls back to
// :latest for local iteration.
var baseImageTag = "dev"

const baseImageRepo = "ramseymcgrath/back2base-base"

// resolveBaseImage returns the fully-qualified base image reference that the
// compose runtime should build on top of. Precedence:
//  1. BACK2BASE_BASE_IMAGE env var (explicit caller override — supports local
//     testing against a custom image).
//  2. The binary's embedded baseImageTag (release builds pin to the matching
//     semver tag published by .github/workflows/base-image.yml on that tag).
//  3. :latest — dev builds and any release whose ldflag wiring drops out.
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
// Called before compose build/run so the pinned-by-version contract is
// real: a user on back2base v0.17.1 will end up running on
// back2base-base:v0.17.1 even if their daemon has never seen that tag.
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
