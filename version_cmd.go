package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(Banner())
		fmt.Printf("   oss-back2base %s (%s) built %s [%s/%s]\n",
			version, commit[:min(7, len(commit))], date,
			runtime.GOOS, runtime.GOARCH)
		fmt.Printf("   base image: %s\n\n", resolveBaseImage())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
