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

type Package struct {
	ID      int
	Generic bool
	WebPath string
	Name    string
	Version string
	Files   []string
}

type Link struct {
	Name    string
	ID      *int
	Package *Package
	File    *string
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

func InferProjectID(path string) (string, errors.E) {
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

func ProjectMilestones(client *gitlab.Client, projectID string) ([]string, errors.E) {
	milestones := []string{}
	options := &gitlab.ListMilestonesOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}
	for {
		page, response, err := client.Milestones.ListMilestones(projectID, options)
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

func PackageFiles(client *gitlab.Client, projectID, packageName string, packageID int) ([]string, errors.E) {
	files := []string{}
	options := &gitlab.ListPackageFilesOptions{
		PerPage: 100,
		Page:    1,
	}
	for {
		page, response, err := client.Packages.ListPackageFiles(projectID, packageID, options)
		if err != nil {
			return nil, errors.Wrapf(err, `failed to list GitLab files for package "%s", page %d`, packageName, options.Page)
		}

		for _, file := range page {
			files = append(files, file.FileName)
		}

		if response.NextPage == 0 {
			break
		}

		options.Page = response.NextPage
	}
	return files, nil
}

func ProjectPackages(client *gitlab.Client, projectID string) ([]Package, errors.E) {
	packages := []Package{}
	options := &gitlab.ListProjectPackagesOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}
	for {
		page, response, err := client.Packages.ListProjectPackages(projectID, options)
		if err != nil {
			return nil, errors.Wrapf(err, `failed to list GitLab packages, page %d`, options.Page)
		}

		for _, p := range page {
			if p.PackageType == "generic" {
				files, err := PackageFiles(client, projectID, p.Name, p.ID)
				if err != nil {
					return nil, err
				}
				packages = append(packages, Package{
					ID:      p.ID,
					Generic: true,
					WebPath: p.Links.WebPath,
					Name:    p.Name,
					Version: p.Version,
					Files:   files,
				})
			} else {
				packages = append(packages, Package{
					ID:      p.ID,
					Generic: false,
					WebPath: p.Links.WebPath,
					Name:    p.Name,
					Version: p.Version,
				})
			}
		}

		if response.NextPage == 0 {
			break
		}

		options.Page = response.NextPage
	}
	return packages, nil
}

func ReleaseLinks(client *gitlab.Client, projectID string, release Release) ([]Link, errors.E) {
	links := []Link{}
	options := &gitlab.ListReleaseLinksOptions{
		PerPage: 100,
		Page:    1,
	}
	for {
		page, response, err := client.ReleaseLinks.ListReleaseLinks(projectID, release.Tag, options)
		if err != nil {
			return nil, errors.Wrapf(err, `failed to list GitLab release links for tag "%s", page %d`, release.Tag, options.Page)
		}

		for _, link := range page {
			links = append(links, Link{
				Name:    link.Name,
				ID:      &link.ID,
				Package: nil,
				File:    nil,
			})
		}

		if response.NextPage == 0 {
			break
		}

		options.Page = response.NextPage
	}
	return links, nil
}

func SyncLinks(client *gitlab.Client, baseURL, projectID string, release Release, packages []Package) errors.E {
	// We remove trailing "/", if it exists.
	if strings.HasSuffix(baseURL, "/") {
		baseURL = baseURL[:len(baseURL)-1]
	}
	links, err := ReleaseLinks(client, projectID, release)
	if err != nil {
		return err
	}
	existingLinks := map[string]Link{}
	for _, link := range links {
		existingLinks[link.Name] = link
	}
	expectedLinks := map[string]Link{}
	for _, p := range packages {
		if p.Generic {
			for _, file := range p.Files {
				name := p.Name + "/" + file
				expectedLinks[name] = Link{
					Name:    name,
					ID:      nil,
					Package: &p,
					File:    &file,
				}
			}
		} else {
			expectedLinks[p.Name] = Link{
				Name:    p.Name,
				ID:      nil,
				Package: &p,
				File:    nil,
			}
		}
	}

	for name, link := range expectedLinks {
		existingLink, ok := existingLinks[name]
		if ok {
			fmt.Printf("Updating GitLab link \"%s\" for release \"%s\".\n", link.Name, release.Tag)
			options := &gitlab.UpdateReleaseLinkOptions{
				Name: &name,
			}
			if link.File == nil {
				options.LinkType = gitlab.LinkType(gitlab.PackageLinkType)
				options.URL = gitlab.String(baseURL + link.Package.WebPath)
				options.FilePath = nil
			} else {
				url := fmt.Sprintf(
					"%s/api/v4/projects/%s/packages/generic/%s/%s/%s",
					baseURL,
					pathEscape(projectID),
					pathEscape(link.Package.Name),
					pathEscape(link.Package.Version),
					pathEscape(*link.File),
				)
				options.LinkType = gitlab.LinkType(gitlab.OtherLinkType)
				options.URL = &url
				options.FilePath = &name
			}
			_, _, err := client.ReleaseLinks.UpdateReleaseLink(projectID, release.Tag, *existingLink.ID, options)
			if err != nil {
				return errors.Wrapf(err, `failed to update GitLab link "%s" for release "%s"`, link.Name, release.Tag)
			}
		} else {
			fmt.Printf("Creating GitLab link \"%s\" for release \"%s\".\n", link.Name, release.Tag)
			options := &gitlab.CreateReleaseLinkOptions{
				Name: &name,
			}
			if link.File == nil {
				options.LinkType = gitlab.LinkType(gitlab.PackageLinkType)
				options.URL = gitlab.String(baseURL + link.Package.WebPath)
				options.FilePath = nil
			} else {
				url := fmt.Sprintf(
					"%s/api/v4/projects/%s/packages/generic/%s/%s/%s",
					baseURL,
					pathEscape(projectID),
					pathEscape(link.Package.Name),
					pathEscape(link.Package.Version),
					pathEscape(*link.File),
				)
				options.LinkType = gitlab.LinkType(gitlab.OtherLinkType)
				options.URL = &url
				options.FilePath = &name
			}
			_, _, err := client.ReleaseLinks.CreateReleaseLink(projectID, release.Tag, options)
			if err != nil {
				return errors.Wrapf(err, `failed to create GitLab link "%s" for release "%s"`, link.Name, release.Tag)
			}
		}
	}

	for name, link := range existingLinks {
		_, ok := expectedLinks[name]
		if !ok {
			fmt.Printf("Deleting GitLab link \"%s\" for release \"%s\".\n", link.Name, release.Tag)
			_, _, err := client.ReleaseLinks.DeleteReleaseLink(projectID, release.Tag, *link.ID)
			if err != nil {
				return errors.Wrapf(err, `failed to delete GitLab link "%s" for release "%s"`, link.Name, release.Tag)
			}
		}
	}

	return nil
}

func Sync(client *gitlab.Client, baseURL, projectID string, release Release, milestones []string, packages []Package) errors.E {
	name := release.Tag
	if release.Yanked {
		name += " [YANKED]"
	}

	description := "<!-- Automatically generated by gitlab.com/tozd/gitlab/release tool. DO NOT EDIT. -->\n\n" + release.Changes

	_, response, err := client.Releases.GetRelease(projectID, release.Tag)
	if response.StatusCode == http.StatusNotFound {
		fmt.Printf("Creating GitLab release for tag \"%s\".\n", release.Tag)
		_, _, err = client.Releases.CreateRelease(projectID, &gitlab.CreateReleaseOptions{
			Name:        &name,
			TagName:     &release.Tag,
			Description: &description,
			ReleasedAt:  &release.Date,
			Milestones:  &milestones,
		})
		if err != nil {
			return errors.Wrapf(err, `failed to create GitLab release for tag "%s"`, release.Tag)
		}
	} else if err != nil {
		return errors.Wrapf(err, `failed to get GitLab release for tag "%s"`, release.Tag)
	} else {
		fmt.Printf("Updating GitLab release for tag \"%s\".\n", release.Tag)
		_, _, err = client.Releases.UpdateRelease(projectID, release.Tag, &gitlab.UpdateReleaseOptions{
			Name:        &name,
			Description: &description,
			ReleasedAt:  &release.Date,
			Milestones:  &milestones,
		})
		if err != nil {
			return errors.Wrapf(err, `failed to update GitLab release for tag "%s"`, release.Tag)
		}
	}

	return SyncLinks(client, baseURL, projectID, release, packages)
}

func DeleteAllExcept(client *gitlab.Client, projectID string, releases []Release) errors.E {
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
		page, response, err := client.Releases.ListReleases(projectID, options)
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
		_, _, err := client.Releases.DeleteRelease(projectID, tag.(string))
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

func MapPackagesToTags(packages []Package, releases []Release) map[string][]Package {
	tagsToPackages := map[string][]Package{}

	tags := make([]string, len(releases))
	for i := 0; i < len(releases); i++ {
		tags[i] = releases[i].Tag
	}

	// First we do a regular sort, so that we get deterministic results later on.
	sort.Stable(sort.StringSlice(tags))
	sort.SliceStable(packages, func(i, j int) bool {
		return packages[i].Version < packages[j].Version
	})
	// Then we sort by length, so that we can map longer tag names first
	// (e.g., 1.0.0-rc before 1.0.0).
	sort.SliceStable(tags, func(i, j int) bool {
		return len(tags[i]) > len(tags[j])
	})

	assignedPackages := mapset.NewThreadUnsafeSet()
	for _, removePrefix := range []bool{false, true} {
		for _, tag := range tags {
			t := tag
			if removePrefix {
				// Removes "v" prefix.
				t = t[1:]
			}

			for _, p := range packages {
				if assignedPackages.Contains(p.ID) {
					continue
				}

				if strings.Contains(p.Version, t) {
					if tagsToPackages[tag] == nil {
						tagsToPackages[tag] = []Package{}
					}
					tagsToPackages[tag] = append(tagsToPackages[tag], p)
					assignedPackages.Add(p.ID)
				}
			}
		}
	}

	return tagsToPackages
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
		projectID, err := InferProjectID(".")
		if err != nil {
			return err
		}
		config.Project = projectID
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

	packages, err := ProjectPackages(client, config.Project)
	if err != nil {
		return err
	}

	tagsToPackages := MapPackagesToTags(packages, releases)

	for _, release := range releases {
		err = Sync(client, config.BaseURL, config.Project, release, tagsToMilestones[release.Tag], tagsToPackages[release.Tag])
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
