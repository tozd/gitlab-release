# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2023-09-24

### Fixed

- E2E tests.

## [0.5.0] - 2023-09-24

### Added

- `--no-create` CLI flag to only update or remove releases, and do not create them.

## [0.4.0] - 2023-09-24

### Changed

- Improve errors.

## [0.3.3] - 2023-09-24

### Fixed

- Fix releases marked erroneously as historical releases.
  [#7](https://gitlab.com/tozd/gitlab/release/-/issues/7)

## [0.3.2] - 2023-09-24

### Fixed

- Another attempt at not make historical releases for new releases.
  [#7](https://gitlab.com/tozd/gitlab/release/-/issues/7)

## [0.3.1] - 2023-09-24

### Fixed

- Do not make historical releases for new releases.
  [#7](https://gitlab.com/tozd/gitlab/release/-/issues/7)

## [0.3.0] - 2022-01-03

### Changed

- Change license to Apache 2.0.

## [0.2.1] - 2021-12-13

### Fixed

- Do not attempt to obtain milestones, packages, and Docker images if they are disabled.

## [0.2.0] - 2021-12-12

### Changed

- Renamed environment variable for token from `CI_JOB_TOKEN` to `GITLAB_API_TOKEN`.
- Mapping milestones, packages, and Docker images to tags also attempts to map
  using a slugified tag.

## [0.1.0] - 2021-12-06

### Added

- First public release.

[unreleased]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.6.0...main
[0.6.0]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.5.0...v0.6.0
[0.5.0]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.4.0...v0.5.0
[0.4.0]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.3.3...v0.4.0
[0.3.3]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.3.2...v0.3.3
[0.3.2]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.3.1...v0.3.2
[0.3.1]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.3.0...v0.3.1
[0.3.0]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.2.1...v0.3.0
[0.2.1]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.2.0...v0.2.1
[0.2.0]: https://gitlab.com/tozd/gitlab/release/-/compare/v0.1.0...v0.2.0
[0.1.0]: https://gitlab.com/tozd/gitlab/release/-/tags/v0.1.0

<!-- markdownlint-disable-file MD024 -->
