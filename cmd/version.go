package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version   = "1.3.0"
	commit    = "unknown"
	buildDate = "unknown"
	date      = "unknown"
	goVersion = runtime.Version()
)

func SetBuildInfo(v, c, d string) {
	if v != "" {
		version = v
	}
	if c != "" {
		commit = c
	}
	if d != "" {
		buildDate = d
		date = d
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show build version information",
	Run: func(cmd *cobra.Command, args []string) {
		if date == "unknown" && buildDate != "unknown" {
			date = buildDate
		}
		fmt.Printf("sshops v%s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("built:  %s\n", date)
		fmt.Printf("go:     %s\n", goVersion)
	},
}
