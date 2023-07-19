package release

import (
	"github.com/alecthomas/kong"
)

// We do not use type=path for Changelog because we want a relative path.

// Config provides configuration.
// It is used as configuration for Kong command-line parser as well.
type Config struct {
	ChangeTo  kong.ChangeDirFlag `env:"CI_PROJECT_DIR"                    help:"Run as if the program was started in PATH instead of the current working directory. Environment variable: ${env}"                                          placeholder:"PATH"                                                                             short:"C"`
	Version   kong.VersionFlag   `help:"Show program's version and exit." short:"V"`
	Project   string             `env:"CI_PROJECT_ID"                     help:"GitLab project to release to. It can be project ID or <namespace/project_path>. By default it infers it from the repository. Environment variable: ${env}" short:"p"`
	BaseURL   string             `default:"https://gitlab.com"            env:"CI_SERVER_URL"                                                                                                                                              help:"Base URL for GitLab API to use. Default is \"${default}\". Environment variable: ${env}" name:"base" placeholder:"URL" short:"B"`
	Token     string             `env:"GITLAB_API_TOKEN"                  help:"GitLab API token to use. Environment variable: ${env}"                                                                                                     required:""                                                                                    short:"t"`
	Changelog string             `default:"CHANGELOG.md"                  help:"Path to the changelog file to use. Default is \"${default}\"."                                                                                             placeholder:"PATH"                                                                             short:"f"`
}
