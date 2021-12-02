package release

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	giturls "github.com/whilp/git-urls"
	gitlab "github.com/xanzy/go-gitlab"
	changelog "github.com/xmidt-org/gokeepachangelog"
	"gitlab.com/tozd/go/errors"
)

type Release struct {
	Tag     string
	Date    time.Time
	Changes string
	Yanked  bool
}

func ChangelogReleases(path string) ([]Release, errors.E) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, `cannot read changelog at "%s"`, path)
	}
	defer file.Close()
	c, err := changelog.Parse(file)
	if err != nil {
		return nil, errors.Wrapf(err, `cannot parse changelog at "%s"`, path)
	}
	releases := make([]Release, 0, len(c.Releases))
	for _, release := range c.Releases {
		if strings.ToLower(release.Version) == "unreleased" {
			continue
		}
		if strings.HasPrefix(release.Version, "v") {
			return nil, errors.Errorf(`release "%s" in the changelog starts with "v", but it should not`, release.Version)
		}
		if release.Date == nil {
			return nil, errors.New(`release "%s" in the changelog is missing date`)
		}

		releases = append(releases, Release{
			Tag:     "v" + release.Version,
			Date:    *release.Date,
			Changes: strings.Join(release.Body[1:], "\n"),
			Yanked:  release.Yanked,
		})
	}
	return releases, nil
}

func GitTags(path string) ([]string, errors.E) {
	repository, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, errors.Wrap(err, `cannot open git repository`)
	}

	tagRefs, err := repository.Tags()
	if err != nil {
		return nil, errors.Wrap(err, `cannot obtain git tags`)
	}

	tags := []string{}
	err = tagRefs.ForEach(func(ref *plumbing.Reference) error {
		tags = append(tags, ref.Name().Short())
		return nil
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return tags, nil
}

func InferProjectId(path string) (string, errors.E) {
	repository, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: true,
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

	if strings.HasSuffix(url.Path, ".git") {
		return url.Path[0 : len(url.Path)-4], nil
	}

	return url.Path, nil
}

func CompareReleasesTags(releases []Release, tags []string) errors.E {
	allReleases := mapset.NewThreadUnsafeSet()
	for _, release := range releases {
		allReleases.Add(release.Tag)
	}

	allTags := mapset.NewThreadUnsafeSet()
	for _, tag := range tags {
		allTags.Add(tag)
	}

	extraReleases := allReleases.Difference(allTags)
	if extraReleases.Cardinality() > 0 {
		return errors.Errorf(`found changelog releases not among git tags: %s`, join(extraReleases.ToSlice(), ", "))
	}

	extraTags := allTags.Difference(allReleases)
	if extraTags.Cardinality() > 0 {
		return errors.Errorf(`found git tags not among changelog releases: %s`, join(extraTags.ToSlice(), ", "))
	}

	return nil
}

func ProjectMilestones(client *gitlab.Client, projectId string) ([]string, errors.E) {
	milestones := []string{}
	options := &gitlab.ListMilestonesOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}
	for {
		page, response, err := client.Milestones.ListMilestones(projectId, options)
		if err != nil {
			return nil, errors.Wrapf(err, `failed to list GitLab milestones, page %d`, options.Page)
		}

		for _, milestone := range page {
			milestones = append(milestones, milestone.Title)
		}

		if response.NextPage == 0 {
			break
		}

		options.Page = response.NextPage
	}
	return milestones, nil
}

func Sync(client *gitlab.Client, projectId string, release Release, milestones []string) errors.E {
	name := release.Tag
	if release.Yanked {
		name += " [YANKED]"
	}

	_, response, err := client.Releases.GetRelease(projectId, release.Tag)
	if response.StatusCode == http.StatusNotFound {
		fmt.Printf("Creating GitLab release for tag \"%s\".\n", release.Tag)
		_, _, err = client.Releases.CreateRelease(projectId, &gitlab.CreateReleaseOptions{
			Name:        &name,
			TagName:     &release.Tag,
			Description: &release.Changes,
			ReleasedAt:  &release.Date,
			Milestones:  milestones,
		})
		return errors.Wrapf(err, `failed to create GitLab release for tag "%s"`, release.Tag)
	} else if err != nil {
		return errors.Wrapf(err, `failed to get GitLab release for tag "%s"`, release.Tag)
	}

	fmt.Printf("Updating GitLab release for tag \"%s\".\n", release.Tag)
	_, _, err = client.Releases.UpdateRelease(projectId, release.Tag, &gitlab.UpdateReleaseOptions{
		Name:        &name,
		Description: &release.Changes,
		ReleasedAt:  &release.Date,
		Milestones:  milestones,
	})
	return errors.Wrapf(err, `failed to update GitLab release for tag "%s"`, release.Tag)
}

func DeleteAllExcept(client *gitlab.Client, projectId string, releases []Release) errors.E {
	allReleases := mapset.NewThreadUnsafeSet()
	for _, release := range releases {
		allReleases.Add(release.Tag)
	}

	allGitLabReleases := mapset.NewThreadUnsafeSet()
	options := &gitlab.ListReleasesOptions{
		PerPage: 100,
		Page:    1,
	}
	for {
		page, response, err := client.Releases.ListReleases(projectId, options)
		if err != nil {
			return errors.Wrapf(err, `failed to list GitLab releases, page %d`, options.Page)
		}

		for _, release := range page {
			allGitLabReleases.Add(release.TagName)
		}

		if response.NextPage == 0 {
			break
		}

		options.Page = response.NextPage
	}

	extraGitLabReleases := allGitLabReleases.Difference(allReleases)
	for _, tag := range extraGitLabReleases.ToSlice() {
		fmt.Printf("Deleting GitLab release for tag \"%s\".\n", tag)
		_, _, err := client.Releases.DeleteRelease(projectId, tag.(string))
		if err != nil {
			return errors.Wrapf(err, `failed to delete GitLab release for tag "%s"`, tag)
		}
	}

	return nil
}

func MapMilestonesToTags(milestones []string, releases []Release) map[string][]string {
	tagsToMilestones := map[string][]string{}

	tags := make([]string, len(releases))
	for i := 0; i < len(releases); i++ {
		tags[i] = releases[i].Tag
	}

	// First we do a regular sort, so that we get deterministic results later on.
	sort.Stable(sort.StringSlice(tags))
	sort.Stable(sort.StringSlice(milestones))
	// Then we sort by length, so that we can map longer tag names first
	// (e.g., 1.0.0-rc before 1.0.0).
	sort.SliceStable(tags, func(i, j int) bool {
		return len(tags[i]) > len(tags[j])
	})

	assignedMilestones := mapset.NewThreadUnsafeSet()
	for _, removePrefix := range []bool{false, true} {
		for _, tag := range tags {
			t := tag
			if removePrefix {
				// Removes "v" prefix.
				t = t[1:]
			}

			for _, milestone := range milestones {
				if assignedMilestones.Contains(milestone) {
					continue
				}

				if strings.Contains(milestone, t) {
					if tagsToMilestones[tag] == nil {
						tagsToMilestones[tag] = []string{}
					}
					tagsToMilestones[tag] = append(tagsToMilestones[tag], milestone)
					assignedMilestones.Add(milestone)
				}
			}
		}
	}

	return tagsToMilestones
}

func SyncAll(config Config) errors.E {
	if config.ChangeTo != "" {
		err := os.Chdir(config.ChangeTo)
		if err != nil {
			return errors.Wrapf(err, `cannot change current working directory to "%s"`, config.ChangeTo)
		}
	}

	releases, err := ChangelogReleases(config.Changelog)
	if err != nil {
		return err
	}

	tags, err := GitTags(".")
	if err != nil {
		return err
	}

	err = CompareReleasesTags(releases, tags)
	if err != nil {
		return err
	}

	if config.Project == "" {
		projectId, err := InferProjectId(".")
		if err != nil {
			return err
		}
		config.Project = projectId
	}

	client, err2 := gitlab.NewClient(config.Token, gitlab.WithBaseURL(config.BaseURL))
	if err2 != nil {
		return errors.Wrap(err2, `failed to create GitLab API client instance`)
	}

	milestones, err := ProjectMilestones(client, config.Project)
	if err != nil {
		return err
	}

	tagsToMilestones := MapMilestonesToTags(milestones, releases)

	for _, release := range releases {
		err = Sync(client, config.Project, release, tagsToMilestones[release.Tag])
		if err != nil {
			return err
		}
	}

	err = DeleteAllExcept(client, config.Project, releases)
	if err != nil {
		return err
	}

	return nil
}
