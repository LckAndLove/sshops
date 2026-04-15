package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

const Version = "1.0.0"

var (
	buildVersion = Version
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func SetBuildInfo(v, c, d string) {
	if v != "" {
		buildVersion = v
	}
	if c != "" {
		buildCommit = c
	}
	if d != "" {
		buildDate = d
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("sshops v%s\n", buildVersion)
		fmt.Printf("commit: %s\n", buildCommit)
		fmt.Printf("built:  %s\n", buildDate)
		fmt.Printf("go:     %s\n", runtime.Version())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
