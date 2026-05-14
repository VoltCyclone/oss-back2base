package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// selectOverviewWithDefault prompts on stderr and reads y/n from the
// supplied reader. EOF or empty input returns defaultValue. Unrecognised
// input is treated as the default rather than erroring — keeps the launcher
// resilient to typos at startup.
func selectOverviewWithDefault(in io.Reader, out io.Writer, defaultValue bool) (bool, error) {
	hint := "[y/N, Enter for N]"
	if defaultValue {
		hint = "[Y/n, Enter for Y]"
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Pre-launch repo overview? (~30-60s, runs Claude over /workspace) %s: ", hint)

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return defaultValue, nil
	}
	switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
	case "":
		return defaultValue, nil
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return defaultValue, nil
	}
}
