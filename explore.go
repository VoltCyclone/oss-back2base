package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// exploreCmd is the launch shortcut for the `/cbox:explore-memory` slash command.
// It's a thin wrapper over `back2base run --prompt "/cbox:explore-memory <path>"`
// so users can kick off a workspace-wide memory exploration without typing the
// full slash-command invocation inside a session. One-shot mode — the container
// exits after the command finishes. Arg is passed through as the target path.
var exploreCmd = &cobra.Command{
	Use:   "explore [path]",
	Short: "Fan out subagents to index each subfolder of the workspace into project-type memories",
	Long: `Launches a back2base session, dispatches /cbox:explore-memory against the
target path (default /workspace), waits for the in-container subagents to
finish writing one project-type memory per subfolder, and exits. The memories
are picked up by memory-mcp-watcher and mirrored to R2 so future sessions
start warm.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}

		// Inject the slash command as the one-shot prompt for runClaude.
		// The flag already exists on rootCmd; overwriting it here is equivalent
		// to the user typing `-p "/cbox:explore-memory <path>"`.
		if arg == "" {
			flagPrompt = "/cbox:explore-memory"
		} else {
			flagPrompt = fmt.Sprintf("/cbox:explore-memory %s", arg)
		}
		return runClaude(cmd, nil)
	},
}

func init() {
	rootCmd.AddCommand(exploreCmd)
}
