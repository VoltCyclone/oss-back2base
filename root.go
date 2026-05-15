package main

import (
	"fmt"
	"os"
	"github.com/spf13/cobra"
)

var (
	flagDirs          []string
	flagPrompt        string
	flagRepo          string
	flagYes           bool
	flagProfile       string
	flagOverview      = false
	flagNoOverview    = false
	flagNamespace     string
	flagNoUpdateCheck bool
)

var rootCmd = &cobra.Command{
	Use:   "oss-back2base [flags] [prompt]",
	Short: "Containerized Claude Code (OSS)",
	Long: `oss-back2base runs Claude Code in a Docker container with a
configurable MCP server registry, an outbound network firewall, and
persistent state. Configs and profiles live in ~/.config/back2base/.

Update: oss-back2base update`,
	Args:  cobra.ArbitraryArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.Flags().StringArrayVarP(&flagDirs, "dir", "d", nil, "Mount an additional directory (repeatable)")
	rootCmd.Flags().StringVarP(&flagPrompt, "prompt", "p", "", "Run a one-shot prompt and exit")
	rootCmd.Flags().StringVarP(&flagRepo, "repo", "r", "", "Override the default repo mount")
	rootCmd.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false, "Skip confirmation prompts")
	rootCmd.PersistentFlags().BoolVar(&flagNoUpdateCheck, "no-update-check", false, "Skip the once-daily new-release check")
	rootCmd.Flags().StringVar(&flagProfile, "profile", "", "MCP server profile (full, go, frontend, infra, research, minimal)")
	rootCmd.Flags().BoolVar(&flagOverview, "overview", false, "Run a pre-launch repo overview before Claude starts")
	rootCmd.Flags().BoolVar(&flagNoOverview, "no-overview", false, "Skip the pre-launch repo overview (overrides remembered preference)")
	rootCmd.Flags().StringVar(&flagNamespace, "namespace", "", "Memory namespace (overrides auto-derivation from git remote or .back2base/namespace)")


	// Prepend the workspace-aware banner to the default help output so
	// the box contents always reflect the cwd, not a compile-time const.
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		if c == rootCmd {
			fmt.Print(Banner())
		}
		defaultHelp(c, args)
	})

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		home := os.Getenv("HOME")
		_ = home // legacy-launcher detection removed in OSS build

		if shouldRunUpdateCheck(cmd) {
			if info := checkForUpdates(); info != nil {
				fmt.Fprintf(os.Stderr,
					":: update available: v%s -> %s  (run `oss-back2base update`)\n",
					info.Current, info.Latest)
			}
		}
		return nil
	}
}

// shouldRunUpdateCheck returns true when the current invocation should
// perform the once-daily nag check. We skip it for `version`, `update`,
// and `selfupdate` (subcommand or any descendant), and when the user
// passed --no-update-check.
func shouldRunUpdateCheck(cmd *cobra.Command) bool {
	if flagNoUpdateCheck {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "version", "update", "selfupdate":
			return false
		}
	}
	return true
}

func Execute() error {
	return rootCmd.Execute()
}
