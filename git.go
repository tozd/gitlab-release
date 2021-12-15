package release

import (
	"strings"

	"github.com/go-git/go-git/v5"
	giturls "github.com/whilp/git-urls"
	"gitlab.com/tozd/go/errors"
)

// inferProjectID infers a GitLab project ID from "origin" remote of a
// git repository at path.
func inferProjectID(path string) (string, errors.E) {
	repository, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: false,
	})
	if err != nil {
		return "", errors.Wrap(err, `cannot open git repository`)
	}

	remote, err := repository.Remote("origin")
	if err != nil {
		return "", errors.Wrap(err, `cannot obtain git "origin" remote`)
	}

	url, err := giturls.Parse(remote.Config().URLs[0])
	if err != nil {
		return "", errors.Wrapf(err, `cannot parse git "origin" remote URL: %s`, remote.Config().URLs[0])
	}

	url.Path = strings.TrimSuffix(url.Path, ".git")
	url.Path = strings.TrimPrefix(url.Path, "/")

	return url.Path, nil
}
