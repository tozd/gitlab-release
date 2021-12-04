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

const maxGitLabPageSize = 100

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

type link struct {
	Name    string
	ID      *int
	Package *Package
	File    *string
}

func changelogReleases(path string) ([]Release, errors.E) {
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

func gitTags(path string) ([]string, errors.E) {
	repository, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: false,
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

	if strings.HasSuffix(url.Path, ".git") {
		return url.Path[0 : len(url.Path)-4], nil
	}

	return url.Path, nil
}

func compareReleasesTags(releases []Release, tags []string) errors.E {
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

func projectMilestones(client *gitlab.Client, projectID string) ([]string, errors.E) {
	milestones := []string{}
	options := &gitlab.ListMilestonesOptions{ //nolint:exhaustivestruct
		ListOptions: gitlab.ListOptions{
			PerPage: maxGitLabPageSize,
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

func packageFiles(client *gitlab.Client, projectID, packageName string, packageID int) ([]string, errors.E) {
	files := []string{}
	options := &gitlab.ListPackageFilesOptions{
		PerPage: maxGitLabPageSize,
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

func projectPackages(client *gitlab.Client, projectID string) ([]Package, errors.E) {
	packages := []Package{}
	options := &gitlab.ListProjectPackagesOptions{ //nolint:exhaustivestruct
		ListOptions: gitlab.ListOptions{
			PerPage: maxGitLabPageSize,
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
				files, err := packageFiles(client, projectID, p.Name, p.ID)
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
					Name:    p.PackageType + "/" + p.Name,
					Version: p.Version,
					Files:   nil,
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

func projectImages(client *gitlab.Client, projectID string) ([]string, errors.E) {
	images := []string{}
	options := &gitlab.ListRegistryRepositoriesOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: maxGitLabPageSize,
			Page:    1,
		},
		Tags:      gitlab.Bool(true),
		TagsCount: nil,
	}
	for {
		page, response, err := client.ContainerRegistry.ListRegistryRepositories(projectID, options)
		if err != nil {
			return nil, errors.Wrapf(err, `failed to list GitLab images, page %d`, options.Page)
		}

		for _, registry := range page {
			for _, tag := range registry.Tags {
				images = append(images, tag.Location)
			}
		}

		if response.NextPage == 0 {
			break
		}

		options.Page = response.NextPage
	}
	return images, nil
}

func releaseLinks(client *gitlab.Client, projectID string, release Release) ([]link, errors.E) {
	links := []link{}
	options := &gitlab.ListReleaseLinksOptions{
		PerPage: maxGitLabPageSize,
		Page:    1,
	}
	for {
		page, response, err := client.ReleaseLinks.ListReleaseLinks(projectID, release.Tag, options)
		if err != nil {
			return nil, errors.Wrapf(err, `failed to list GitLab release links for tag "%s", page %d`, release.Tag, options.Page)
		}

		for _, l := range page {
			links = append(links, link{
				Name:    l.Name,
				ID:      &l.ID,
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

func syncLinks(client *gitlab.Client, baseURL, projectID string, release Release, packages []Package) errors.E {
	// We remove trailing "/", if it exists.
	baseURL = strings.TrimSuffix(baseURL, "/")
	links, err := releaseLinks(client, projectID, release)
	if err != nil {
		return err
	}
	existingLinks := map[string]link{}
	for _, l := range links {
		existingLinks[l.Name] = l
	}
	expectedLinks := map[string]link{}
	for i := range packages {
		// We create our own p because later on we take an address of p
		// and we do not want to have an implicit memory aliasing in for loop.
		p := packages[i]
		if p.Generic {
			for j := range p.Files {
				// We create our own file because later on we take an address of file
				// and we do not want to have an implicit memory aliasing in for loop.
				file := p.Files[j]
				name := p.Name + "/" + file
				expectedLinks[name] = link{
					Name:    name,
					ID:      nil,
					Package: &p,
					File:    &file,
				}
			}
		} else {
			expectedLinks[p.Name] = link{
				Name:    p.Name,
				ID:      nil,
				Package: &p,
				File:    nil,
			}
		}
	}

	for name, l := range existingLinks {
		_, ok := expectedLinks[name]
		if !ok {
			fmt.Printf("Deleting GitLab link \"%s\" for release \"%s\".\n", l.Name, release.Tag)
			_, _, err := client.ReleaseLinks.DeleteReleaseLink(projectID, release.Tag, *l.ID)
			if err != nil {
				return errors.Wrapf(err, `failed to delete GitLab link "%s" for release "%s"`, l.Name, release.Tag)
			}
		}
	}

	for name, l := range expectedLinks {
		existingLink, ok := existingLinks[name]
		if ok {
			fmt.Printf("Updating GitLab link \"%s\" for release \"%s\".\n", l.Name, release.Tag)
			options := &gitlab.UpdateReleaseLinkOptions{ //nolint:exhaustivestruct
				Name: &name,
			}
			if l.File == nil {
				options.URL = gitlab.String(baseURL + l.Package.WebPath)
				options.FilePath = nil
				options.LinkType = gitlab.LinkType(gitlab.PackageLinkType)
			} else {
				url := fmt.Sprintf(
					"%s/api/v4/projects/%s/packages/generic/%s/%s/%s",
					baseURL,
					pathEscape(projectID),
					pathEscape(l.Package.Name),
					pathEscape(l.Package.Version),
					pathEscape(*l.File),
				)
				options.URL = &url
				options.FilePath = gitlab.String("/" + name)
				options.LinkType = gitlab.LinkType(gitlab.OtherLinkType)
			}
			_, _, err := client.ReleaseLinks.UpdateReleaseLink(projectID, release.Tag, *existingLink.ID, options)
			if err != nil {
				return errors.Wrapf(err, `failed to update GitLab link "%s" for release "%s"`, l.Name, release.Tag)
			}
		} else {
			fmt.Printf("Creating GitLab link \"%s\" for release \"%s\".\n", l.Name, release.Tag)
			options := &gitlab.CreateReleaseLinkOptions{ //nolint:exhaustivestruct
				Name: &name,
			}
			if l.File == nil {
				options.URL = gitlab.String(baseURL + l.Package.WebPath)
				options.FilePath = nil
				options.LinkType = gitlab.LinkType(gitlab.PackageLinkType)
			} else {
				url := fmt.Sprintf(
					"%s/api/v4/projects/%s/packages/generic/%s/%s/%s",
					baseURL,
					pathEscape(projectID),
					pathEscape(l.Package.Name),
					pathEscape(l.Package.Version),
					pathEscape(*l.File),
				)
				options.URL = &url
				options.FilePath = gitlab.String("/" + name)
				options.LinkType = gitlab.LinkType(gitlab.OtherLinkType)
			}
			_, _, err := client.ReleaseLinks.CreateReleaseLink(projectID, release.Tag, options)
			if err != nil {
				return errors.Wrapf(err, `failed to create GitLab link "%s" for release "%s"`, l.Name, release.Tag)
			}
		}
	}

	return nil
}

func Sync(
	config *Config, client *gitlab.Client, release Release,
	milestones []string, packages []Package, images []string,
) errors.E {
	name := release.Tag
	if release.Yanked {
		name += " [YANKED]"
	}

	description := "<!-- Automatically generated by gitlab.com/tozd/gitlab/release tool. DO NOT EDIT. -->\n\n"

	// TODO: Improve with official links to Docker images, once they are available.
	//       See: https://gitlab.com/gitlab-org/gitlab/-/issues/346982
	if len(images) > 0 {
		description += "##### Docker images\n"
		for _, image := range images {
			description += "* `" + image + "`\n"
		}
		description += "\n"
	}

	description += release.Changes

	_, response, err := client.Releases.GetRelease(config.Project, release.Tag)
	if response.StatusCode == http.StatusNotFound {
		fmt.Printf("Creating GitLab release for tag \"%s\".\n", release.Tag)
		_, _, err = client.Releases.CreateRelease(config.Project, &gitlab.CreateReleaseOptions{
			Name:        &name,
			TagName:     &release.Tag,
			Description: &description,
			Ref:         nil,
			Milestones:  &milestones,
			// TODO: Provide assets already here.
			//       See: https://github.com/xanzy/go-gitlab/pull/1305
			Assets:     nil,
			ReleasedAt: &release.Date,
		})
		if err != nil {
			return errors.Wrapf(err, `failed to create GitLab release for tag "%s"`, release.Tag)
		}
	} else if err != nil {
		return errors.Wrapf(err, `failed to get GitLab release for tag "%s"`, release.Tag)
	} else {
		fmt.Printf("Updating GitLab release for tag \"%s\".\n", release.Tag)
		_, _, err = client.Releases.UpdateRelease(config.Project, release.Tag, &gitlab.UpdateReleaseOptions{
			Name:        &name,
			Description: &description,
			ReleasedAt:  &release.Date,
			Milestones:  &milestones,
		})
		if err != nil {
			return errors.Wrapf(err, `failed to update GitLab release for tag "%s"`, release.Tag)
		}
	}

	return syncLinks(client, config.BaseURL, config.Project, release, packages)
}

func DeleteAllExcept(config *Config, client *gitlab.Client, releases []Release) errors.E {
	allReleases := mapset.NewThreadUnsafeSet()
	for _, release := range releases {
		allReleases.Add(release.Tag)
	}

	allGitLabReleases := mapset.NewThreadUnsafeSet()
	options := &gitlab.ListReleasesOptions{
		PerPage: maxGitLabPageSize,
		Page:    1,
	}
	for {
		page, response, err := client.Releases.ListReleases(config.Project, options)
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
		_, _, err := client.Releases.DeleteRelease(config.Project, tag.(string))
		if err != nil {
			return errors.Wrapf(err, `failed to delete GitLab release for tag "%s"`, tag)
		}
	}

	return nil
}

func mapStringsToTags(inputs []string, releases []Release) map[string][]string {
	tagsToInputs := map[string][]string{}

	tags := make([]string, len(releases))
	for i := 0; i < len(releases); i++ {
		tags[i] = releases[i].Tag
	}

	// First we do a regular sort, so that we get deterministic results later on.
	sort.Stable(sort.StringSlice(tags))
	sort.Stable(sort.StringSlice(inputs))
	// Then we sort by length, so that we can map longer tag names first
	// (e.g., 1.0.0-rc before 1.0.0).
	sort.SliceStable(tags, func(i, j int) bool {
		return len(tags[i]) > len(tags[j])
	})

	assignedInputs := mapset.NewThreadUnsafeSet()
	for _, removePrefix := range []bool{false, true} {
		for _, tag := range tags {
			t := tag
			if removePrefix {
				// Removes "v" prefix.
				t = t[1:]
			}

			for _, input := range inputs {
				if assignedInputs.Contains(input) {
					continue
				}

				if strings.Contains(input, t) {
					if tagsToInputs[tag] == nil {
						tagsToInputs[tag] = []string{}
					}
					tagsToInputs[tag] = append(tagsToInputs[tag], input)
					assignedInputs.Add(input)
				}
			}
		}
	}

	return tagsToInputs
}

func mapMilestonesToTags(milestones []string, releases []Release) map[string][]string {
	return mapStringsToTags(milestones, releases)
}

func mapPackagesToTags(packages []Package, releases []Release) map[string][]Package {
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

func mapImagesToTags(images []string, releases []Release) map[string][]string {
	return mapStringsToTags(images, releases)
}

func SyncAll(config *Config) errors.E {
	if config.ChangeTo != "" {
		err := os.Chdir(config.ChangeTo)
		if err != nil {
			return errors.Wrapf(err, `cannot change current working directory to "%s"`, config.ChangeTo)
		}
	}

	releases, err := changelogReleases(config.Changelog)
	if err != nil {
		return err
	}

	tags, err := gitTags(".")
	if err != nil {
		return err
	}

	err = compareReleasesTags(releases, tags)
	if err != nil {
		return err
	}

	if config.Project == "" {
		projectID, err := inferProjectID(".") //nolint:govet
		if err != nil {
			return err
		}
		config.Project = projectID
	}

	client, err2 := gitlab.NewClient(config.Token, gitlab.WithBaseURL(config.BaseURL))
	if err2 != nil {
		return errors.Wrap(err2, `failed to create GitLab API client instance`)
	}

	milestones, err := projectMilestones(client, config.Project)
	if err != nil {
		return err
	}

	tagsToMilestones := mapMilestonesToTags(milestones, releases)

	packages, err := projectPackages(client, config.Project)
	if err != nil {
		return err
	}

	tagsToPackages := mapPackagesToTags(packages, releases)

	images, err := projectImages(client, config.Project)
	if err != nil {
		return err
	}

	tagsToImages := mapImagesToTags(images, releases)

	for _, release := range releases {
		err = Sync(
			config, client, release,
			tagsToMilestones[release.Tag], tagsToPackages[release.Tag], tagsToImages[release.Tag],
		)
		if err != nil {
			return err
		}
	}

	err = DeleteAllExcept(config, client, releases)
	if err != nil {
		return err
	}

	return nil
}
