package release

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Changelog is from: https://keepachangelog.com/en/1.0.0/
//
//go:embed testdata/changelog.md
var testChangelog []byte

func mustParse(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05 -0700 MST", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestChangelogReleases(t *testing.T) {
	tempDir := t.TempDir()
	changelogPath := filepath.Join(tempDir, "CHANGELOG.md")
	err := os.WriteFile(changelogPath, testChangelog, 0o600)
	require.NoError(t, err)
	releases, err := changelogReleases(changelogPath)
	require.NoError(t, err)
	for i := range releases {
		releases[i].Changes = ""
	}
	assert.Equal(t, []Release{
		{"v1.0.0", mustParse("2017-06-20 00:00:00 +0000 UTC"), "", false},
		{"v0.3.0", mustParse("2015-12-03 00:00:00 +0000 UTC"), "", false},
		{"v0.2.0", mustParse("2015-10-06 00:00:00 +0000 UTC"), "", false},
		{"v0.1.0", mustParse("2015-10-06 00:00:00 +0000 UTC"), "", false},
		{"v0.0.8", mustParse("2015-02-17 00:00:00 +0000 UTC"), "", false},
		{"v0.0.7", mustParse("2015-02-16 00:00:00 +0000 UTC"), "", false},
		{"v0.0.6", mustParse("2014-12-12 00:00:00 +0000 UTC"), "", false},
		{"v0.0.5", mustParse("2014-08-09 00:00:00 +0000 UTC"), "", false},
		{"v0.0.4", mustParse("2014-08-09 00:00:00 +0000 UTC"), "", false},
		{"v0.0.3", mustParse("2014-08-09 00:00:00 +0000 UTC"), "", false},
		{"v0.0.2", mustParse("2014-07-10 00:00:00 +0000 UTC"), "", false},
		{"v0.0.1", mustParse("2014-05-31 00:00:00 +0000 UTC"), "", false},
	}, releases)
}

func TestGitTags(t *testing.T) {
	tempDir := t.TempDir()
	repository, err := git.PlainInit(tempDir, false)
	require.NoError(t, err)
	workTree, err := repository.Worktree()
	require.NoError(t, err)
	filename := filepath.Join(tempDir, "file.txt")
	author := &object.Signature{
		Name:  "John Doe",
		Email: "john@doe.org",
		When:  time.Now(),
	}
	expectedTags := []string{"v1.0.0", "v2.0.0", "v3.0.0"}
	for i, tag := range expectedTags {
		err := os.WriteFile(filename, []byte("Data: "+tag), 0o600) //nolint:govet
		require.NoError(t, err)
		_, err = workTree.Add("file.txt")
		require.NoError(t, err)
		commit, err := workTree.Commit("Change for "+tag, &git.CommitOptions{
			Author: author,
		})
		require.NoError(t, err)
		var opts *git.CreateTagOptions
		// Mix annotated and lightweight tags.
		if i%2 == 0 {
			opts = &git.CreateTagOptions{
				Tagger:  author,
				Message: tag,
			}
		}
		_, err = repository.CreateTag(tag, commit, opts)
		require.NoError(t, err)
	}
	tags, err := gitTags(tempDir)
	require.NoError(t, err)
	assert.ElementsMatch(t, expectedTags, tags)
}

func TestCompareReleasesTags(t *testing.T) {
	err := compareReleasesTags(
		[]Release{},
		[]string{},
	)
	assert.NoError(t, err)

	err = compareReleasesTags(
		[]Release{{Tag: "v1.0.0"}},
		[]string{"v1.0.0"},
	)
	assert.NoError(t, err)

	err = compareReleasesTags(
		[]Release{{Tag: "v1.0.0"}},
		[]string{"v2.0.0"},
	)
	assert.EqualError(t, err, `found changelog releases not among git tags: v1.0.0`)

	err = compareReleasesTags(
		[]Release{{Tag: "v1.0.0"}},
		[]string{"v1.0.0", "v2.0.0"},
	)
	assert.EqualError(t, err, `found git tags not among changelog releases: v2.0.0`)
}

func toStringsMap(inputs []string, tags []string) map[string][]string {
	releases := make([]Release, len(tags))
	for i, tag := range tags {
		releases[i] = Release{Tag: tag}
	}
	return mapStringsToTags(inputs, releases)
}

func toPackagesMap(inputs []string, tags []string) map[string][]string {
	packages := make([]Package, len(inputs))
	for i, p := range inputs {
		packages[i] = Package{ID: i, Version: p}
	}
	releases := make([]Release, len(tags))
	for i, tag := range tags {
		releases[i] = Release{Tag: tag}
	}
	result := map[string][]string{}
	for tag, packages := range mapPackagesToTags(packages, releases) {
		result[tag] = make([]string, len(packages))
		for i, p := range packages {
			result[tag][i] = p.Version
		}
	}
	return result
}

func TestMappingToTags(t *testing.T) {
	mappingFuncs := []struct {
		name string
		f    func([]string, []string) map[string][]string
	}{
		{"mapStringsToTags", toStringsMap},
		{"mapPackagesToTags", toPackagesMap},
	}

	tests := []struct {
		inputs  []string
		tags    []string
		mapping map[string][]string
	}{
		{[]string{}, []string{}, map[string][]string{}},
		{[]string{"1.0.0-rc", "1.0.0", "2.0.0"}, []string{}, map[string][]string{}},
		{
			[]string{"1.0.0-rc", "1.0.0", "2.0.0"},
			[]string{"v1.0.0", "v2.0.0"},
			map[string][]string{
				"v1.0.0": {"1.0.0", "1.0.0-rc"},
				"v2.0.0": {"2.0.0"},
			},
		},
		{
			[]string{"1.0.0-rc", "1.0.0", "2.0.0"},
			[]string{"v1.0.0", "v1.0.0-rc", "v2.0.0"},
			map[string][]string{
				"v1.0.0":    {"1.0.0"},
				"v1.0.0-rc": {"1.0.0-rc"},
				"v2.0.0":    {"2.0.0"},
			},
		},
	}

	for _, ff := range mappingFuncs {
		t.Run(fmt.Sprintf("case=%s", ff.name), func(t *testing.T) {
			for k, tt := range tests {
				t.Run(fmt.Sprintf("case=%d", k), func(t *testing.T) {
					assert.Equal(t, tt.mapping, ff.f(tt.inputs, tt.tags))
				})
			}
		})
	}
}
