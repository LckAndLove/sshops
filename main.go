package main

import "github.com/yourname/sshops/cmd"

var (
	version   = "1.1.0"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	cmd.SetBuildInfo(version, commit, buildDate)
	cmd.Execute()
}
