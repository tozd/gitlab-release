# Automatic GitLab releases

[![Go Report Card](https://goreportcard.com/badge/gitlab.com/tozd/gitlab/release)](https://goreportcard.com/report/gitlab.com/tozd/gitlab/release)
[![pipeline status](https://gitlab.com/tozd/gitlab/release/badges/main/pipeline.svg?ignore_skipped=true)](https://gitlab.com/tozd/gitlab/release/-/pipelines)
[![coverage report](https://gitlab.com/tozd/gitlab/release/badges/main/coverage.svg)](https://gitlab.com/tozd/gitlab/release/-/graphs/main/charts)

Sync tags in your git repository and a changelog in [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)
format with [releases of your GitLab project](https://about.gitlab.com/releases/categories/releases/).

Features:

* Extracts description of each release entry in a changelog in [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format
  and creates or updates a corresponding
  [GitLab release](https://about.gitlab.com/releases/categories/releases/).
* Any deleted release entry in a changelog removes a corresponding GitLab release, too.
  But consider instead marking a release in the changelog as `[YANKED]`.
* Automatically associates milestones, packages, and Docker images with each release.
* Makes sure your changelog can be parsed as a Keep a Changelog.
* Makes sure all release entries in your changelog have a corresponding git tag and
  all git tags have a corresponding release entry in your changelog.
* Can run as a CI job.

## Installation

This is a tool implemented in Go. You can use `go install` to install the latest development version (`main` branch):

```sh
go install gitlab.com/tozd/gitlab/release@latest
```

[Releases page](https://gitlab.com/tozd/gitlab/release/-/releases)
contains a list of stable versions. Each includes:

* Statically compiled binaries.
* Docker images.

There is also a [read-only GitHub mirror available](https://github.com/tozd/gitlab-release),
if you need to fork the project there.

## Usage

The tool operates automatically and uses defaults which makes it suitable
to run inside the GitLab CI environment. To see configuration options available,
run

```sh
gitlab-release --help
```

You can provide some configuration options as environment variables.

The only required configuration option is the [access token](https://docs.gitlab.com/ee/api/index.html#personalproject-access-tokens)
which you can provide with `-t/--token` command line flag
or `GITLAB_API_TOKEN` environment variable.
Use a [personal access token](https://docs.gitlab.com/ee/user/profile/personal_access_tokens.html)
or [project access token](https://docs.gitlab.com/ee/user/project/settings/project_access_tokens.html) with `api` scope
and permission to manage releases
(at least [developer level](https://docs.gitlab.com/ee/user/project/releases/#release-permissions)
and if you use [protected tags](https://docs.gitlab.com/ee/user/project/protected_tags.html),
the token
[must be allowed to create protected tags](https://docs.gitlab.com/ee/user/project/protected_tags.html#configuring-protected-tags),
too).

The tool automatically associates:

* milestones: if the release version matches the title of the milestone;
  each release can have multiple milestones; each milestone can be associated with multiple releases
* generic packages: if the release version matches generic package's version all files contained inside the generic package
  are associated with the release
* other packages: if the release version matches package's version
* Docker images: if the release version matches the full Docker image name

Version matching is done by searching if the target string contains the version string, with
and without `v` prefix, and with version slugified and not.

### GitLab CI configuration

You can add to your GitLab CI configuration a job like:

```yaml
sync_releases:
  stage: deploy

  image:
    name: registry.gitlab.com/tozd/gitlab/release/branch/main:latest-debug
    entrypoint: [""]

  script:
    - /gitlab-release

  rules:
    - if: '$GITLAB_API_TOKEN && ($CI_COMMIT_BRANCH == "main" || $CI_COMMIT_TAG)'
```

Notes:

* Job runs only when `GITLAB_API_TOKEN` is present (e.g., only on protected branches)
  and only on the `main` branch (e.g., one with the latest stable version of the changelog) or
  when the repository is tagged. Change to suit your needs.
* Configure `GITLAB_API_TOKEN` as [GitLab CI/CD variable](https://docs.gitlab.com/ee/ci/variables/index.html).
  Protected and masked.
* The example above uses the latest version of the tool from the `main` branch.
  Consider using a Docker image corresponding to the
  [latest released stable version](https://gitlab.com/tozd/gitlab/release/-/releases).
* Use of `-debug` Docker image is currently required.
  See [this issue](https://gitlab.com/tozd/gitlab/release/-/issues/4) for more details.

## Releases maintained using this tool

To see how releases look when maintained using this tool, check out these
projects:

* [This project itself](https://gitlab.com/tozd/gitlab/release/-/releases)
* [gitlab-config tool](https://gitlab.com/tozd/gitlab/config/-/releases)
* [`gitlab.com/tozd/go/errors` Go package](https://gitlab.com/tozd/go/errors-/releases)

_Feel free to make a merge-request adding yours to the list._
