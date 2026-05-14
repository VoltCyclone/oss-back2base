package main

import "testing"

func TestResolveBaseImage(t *testing.T) {
	// Clear any ambient override from the user's environment.
	t.Setenv("BACK2BASE_BASE_IMAGE", "")

	cases := []struct {
		name string
		tag  string
		want string
	}{
		{"dev build falls back to latest", "dev", "ramseymcgrath/back2base-base:latest"},
		{"empty tag falls back to latest", "", "ramseymcgrath/back2base-base:latest"},
		{"versioned tag pins", "v0.17.1", "ramseymcgrath/back2base-base:v0.17.1"},
	}
	orig := baseImageTag
	t.Cleanup(func() { baseImageTag = orig })

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseImageTag = tc.tag
			if got := resolveBaseImage(); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestResolveBaseImageEnvOverride(t *testing.T) {
	orig := baseImageTag
	t.Cleanup(func() { baseImageTag = orig })
	baseImageTag = "v0.17.1"

	// Env override beats a pinned build.
	t.Setenv("BACK2BASE_BASE_IMAGE", "localdev/back2base-base:myexperiment")
	if got := resolveBaseImage(); got != "localdev/back2base-base:myexperiment" {
		t.Errorf("env override ignored: %q", got)
	}

	// Unsetting the env returns to the pin. t.Setenv with "" clears for
	// the rest of the test and restores the original after.
	t.Setenv("BACK2BASE_BASE_IMAGE", "")
	if got := resolveBaseImage(); got != "ramseymcgrath/back2base-base:v0.17.1" {
		t.Errorf("pin not restored after unset: %q", got)
	}
}
