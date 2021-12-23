// Command gitlab-release syncs tags in your git repository and a changelog in Keep a Changelog
// format with releases of your GitLab project.
//
// You can provide some configuration options as environment variables.
package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"

	"gitlab.com/tozd/gitlab/release"
)

const exitCode = 2

// These variables should be set during build time using "-X" ldflags.
var (
	version        = ""
	buildTimestamp = ""
	revision       = ""
)

func main() {
	var config release.Config
	ctx := kong.Parse(&config,
		kong.Description(
			"Sync tags in your git repository and a changelog in Keep a Changelog "+
				"format with releases of your GitLab project.\n\n"+
				"You can provide some configuration options as environment variables.",
		),
		kong.Vars{
			"version": fmt.Sprintf("version %s (build on %s, git revision %s)", version, buildTimestamp, revision),
		},
		kong.UsageOnError(),
		kong.Writers(
			os.Stderr,
			os.Stderr,
		),
	)

	err := release.Sync(&config)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "error: %+v", err)
		ctx.Exit(exitCode)
	}
}
