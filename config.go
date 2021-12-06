package release

import (
	"github.com/alecthomas/kong"
)

// We do not use type=path for Changelog because we want a relative path.
// We have to use type=string together with defaults to render placeholder correctly.
// See: https://github.com/alecthomas/kong/issues/243

// Config provides configuration.
// It is used as configuration for Kong command-line parser as well.
type Config struct {
	Changelog string `short:"f" placeholder:"PATH" default:"CHANGELOG.md" type:"string" help:"Path to the changelog file to use. Default is \"${default}\"."`
	Project   string `short:"p" env:"CI_PROJECT_ID" help:"GitLab project to release to. It can be project ID or <namespace/project_path>. By default it infers it from the repository."`
	ChangeTo  string `short:"C" placeholder:"PATH" type:"existingdir" env:"CI_PROJECT_DIR" help:"Run as if the program was started in <path> instead of the current working directory."`
	BaseURL   string `short:"B" name:"base" placeholder:"URL" default:"https://gitlab.com" type:"string" env:"CI_SERVER_URL" help:"Base URL for GitLab API to use. Default is \"${default}\"."`
	Token     string `short:"t" required:"" env:"CI_JOB_TOKEN" help:"GitLab API token to use."`
	// To support "--version" flag.
	Version kong.VersionFlag
}
