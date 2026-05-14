package main

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		current, latest string
		wantNewer       bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.2.0", "0.2.0", false},
		{"0.2.0", "0.1.0", false},
		{"1.0.0", "1.0.1", true},
		{"1.0.10", "1.0.9", false},
		{"dev", "0.1.0", true},
		{"0.1.0", "dev", false},
	}
	for _, tt := range tests {
		t.Run(tt.current+"->"+tt.latest, func(t *testing.T) {
			got := isNewer(tt.current, tt.latest)
			if got != tt.wantNewer {
				t.Errorf("isNewer(%q, %q) = %v, want %v",
					tt.current, tt.latest, got, tt.wantNewer)
			}
		})
	}
}

func TestParseReleaseTag(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"v0.1.0", "0.1.0"},
		{"v1.2.3", "1.2.3"},
		{"0.1.0", "0.1.0"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			got := parseReleaseTag(tt.tag)
			if got != tt.want {
				t.Errorf("parseReleaseTag(%q) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}
