package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ASCII rendition of the back2base brand mark: a container sitting on a
// base plate. The base bar extends past the container, matching the SVG
// logo. The text inside the container is the workspace name (basename of
// the current working directory), truncated to fit the box interior.
const (
	// 24-bit safety orange — matches #ff8a3d in the brand spec.
	ansiOrange = "\x1b[38;2;255;138;61m"
	ansiReset  = "\x1b[0m"

	boxInnerWidth = 22 // chars between the two │ borders
)

var reANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")

// Banner renders the logo with the current workspace (cwd basename) in
// the container. Colorized on TTY, plain under NO_COLOR / TERM=dumb.
func Banner() string {
	return BannerFor(workspaceName())
}

// BannerFor renders the logo with arbitrary text inside the container.
// Use this in contexts that shouldn't track cwd (e.g. global subcommands).
func BannerFor(inner string) string {
	line := centerIn(inner, boxInnerWidth)
	b := "" +
		"     " + ansiOrange + "┌──────────────────────┐" + ansiReset + "\n" +
		"     " + ansiOrange + "│" + ansiReset + strings.Repeat(" ", boxInnerWidth) + ansiOrange + "│" + ansiReset + "\n" +
		"     " + ansiOrange + "│" + ansiReset + line + ansiOrange + "│" + ansiReset + "\n" +
		"     " + ansiOrange + "│" + ansiReset + strings.Repeat(" ", boxInnerWidth) + ansiOrange + "│" + ansiReset + "\n" +
		"     " + ansiOrange + "└──────────────────────┘" + ansiReset + "\n" +
		"   " + ansiOrange + "━━━━━━━━━━━━━━━━━━━━━━━━━━━━" + ansiReset + "\n"
	if supportsColor() {
		return b
	}
	return reANSI.ReplaceAllString(b, "")
}

// centerIn pads (or truncates with ellipsis) s so it fills exactly width
// visible columns. ASCII-only — safe for single-width runes.
func centerIn(s string, width int) string {
	if len(s) > width {
		if width <= 1 {
			return s[:width]
		}
		s = s[:width-1] + "…"
	}
	pad := width - runewidth(s)
	left := pad / 2
	right := pad - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// runewidth counts visible columns. Treat everything as width 1 since we
// only allow the ellipsis as a non-ASCII char and it renders single-width
// in every monospace font we care about.
func runewidth(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func workspaceName() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return "back2base"
	}
	name := filepath.Base(cwd)
	if name == "" || name == "/" || name == "." {
		return "back2base"
	}
	return name
}

func supportsColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
