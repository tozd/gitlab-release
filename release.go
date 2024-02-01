// Package release implements the gitlab-release command.
// It provides functions to sync tags in your git repository and a changelog in Keep a Changelog
// format with releases of your GitLab project.
package release

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/xanzy/go-gitlab"
	changelog "github.com/xmidt-org/gokeepachangelog"
	"gitlab.com/tozd/go/errors"
	"gitlab.com/tozd/go/x"
)

// See: https://docs.gitlab.com/ee/api/#offset-based-pagination
const maxGitLabPageSize = 100

// Release holds information about a release extracted from a
// Keep a Changelog changelog.
type Release struct {
	Tag     string
	Changes string
	Yanked  bool
}

// Tag holds information about a git tag.
type Tag struct {
	Name string
	Date time.Time
}

// Package describes a GitLab project's package.
// Generic packages have files which are listed directly,
// while non-generic packages have a web path to which we just link.
//
// See: https://docs.gitlab.com/ee/user/packages/package_registry/
//
// See: https://docs.gitlab.com/ee/user/packages/generic_packages/index.html
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

// changelogReleases extacts releases from a changelog file at path.
// The changelog should be in the Keep a Changelog format.
func changelogReleases(path string) ([]Release, errors.E) {
	file, err := os.Open(path)
	if err != nil {
		errE := errors.WithMessage(err, "cannot read changelog")
		errors.Details(errE)["path"] = path
		return nil, errE
	}
	defer file.Close()
	c, err := changelog.Parse(file)
	if err != nil {
		errE := errors.WithMessage(err, "cannot parse changelog")
		errors.Details(errE)["path"] = path
		return nil, errE
	}
	releases := make([]Release, 0, len(c.Releases))
	for _, release := range c.Releases {
		if strings.ToLower(release.Version) == "unreleased" {
			continue
		}
		if strings.HasPrefix(release.Version, "v") {
			errE := errors.New(`release in the changelog starts with "v", but it should not`)
			errors.Details(errE)["release"] = release.Version
			return nil, errE
		}
		if release.Date == nil {
			errE := errors.New("release in the changelog is missing date")
			errors.Details(errE)["release"] = release.Version
			return nil, errE
		}

		releases = append(releases, Release{
			Tag:     "v" + release.Version,
			Changes: strings.Join(release.Body[1:], "\n"),
			Yanked:  release.Yanked,
		})
	}
	return releases, nil
}

// gitTags obtains all tags from a git repository at path.
func gitTags(path string) ([]Tag, errors.E) {
	repository, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: false,
	})
	if err != nil {
		errE := errors.WithMessage(err, "cannot open git repository")
		errors.Details(errE)["path"] = path
		return nil, errE
	}

	tagRefs, err := repository.Tags()
	if err != nil {
		errE := errors.WithMessage(err, "cannot obtain git tags")
		errors.Details(errE)["path"] = path
		return nil, errE
	}

	tags := []Tag{}
	err = tagRefs.ForEach(func(ref *plumbing.Reference) error {
		tag, err := repository.TagObject(ref.Hash()) //nolint:govet
		if err != nil && errors.Is(err, plumbing.ErrObjectNotFound) {
			commit, err := repository.CommitObject(ref.Hash()) //nolint:govet
			if err != nil {
				errE := errors.WithMessage(err, "commit object")
				errors.Details(errE)["hash"] = ref.Hash()
				return errE
			}
			tags = append(tags, Tag{
				Name: ref.Name().Short(),
				Date: commit.Committer.When,
			})
		} else if err != nil {
			errE := errors.WithMessage(err, "tag object")
			errors.Details(errE)["hash"] = ref.Hash()
			return errE
		} else {
			tags = append(tags, Tag{
				Name: tag.Name,
				Date: tag.Tagger.When,
			})
		}
		return nil
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return tags, nil
}

// compareReleasesTags returns an error if all releases do not exactly match all tags.
func compareReleasesTags(releases []Release, tags []Tag) errors.E {
	allReleases := mapset.NewThreadUnsafeSet[string]()
	for _, release := range releases {
		allReleases.Add(release.Tag)
	}

	allTags := mapset.NewThreadUnsafeSet[string]()
	for _, tag := range tags {
		allTags.Add(tag.Name)
	}

	extraReleases := allReleases.Difference(allTags)
	if extraReleases.Cardinality() > 0 {
		errE := errors.Errorf("found changelog releases not among git tags")
		releases := extraReleases.ToSlice()
		slices.Sort(releases)
		errors.Details(errE)["releases"] = releases
		return errE
	}

	extraTags := allTags.Difference(allReleases)
	if extraTags.Cardinality() > 0 {
		errE := errors.Errorf("found git tags not among changelog releases")
		tags := extraTags.ToSlice()
		slices.Sort(tags)
		errors.Details(errE)["tags"] = tags
		return errE
	}

	return nil
}

// projectConfiguration fetches configuration of a GitLab projectID project
// and returns if issues, packages, and Docker images are enabled.
func projectConfiguration( //nolint:nonamedreturns
	client *gitlab.Client, projectID string,
) (hasIssues, hasPackages, hasImages bool, errE errors.E) {
	project, _, err := client.Projects.GetProject(projectID, nil)
	if err != nil {
		errE = errors.WithMessage(err, "failed to get GitLab project")
		return
	}

	hasIssues = project.IssuesAccessLevel != gitlab.DisabledAccessControl
	hasPackages = project.RepositoryAccessLevel != gitlab.DisabledAccessControl && project.PackagesEnabled
	hasImages = project.ContainerRegistryAccessLevel != gitlab.DisabledAccessControl
	return
}

// projectMilestones fetches all milestone titles for a GitLab projectID project.
//
// GitLab milestones are uniquely identified by their titles.
func projectMilestones(client *gitlab.Client, projectID string) ([]string, errors.E) {
	milestones := []string{}
	options := &gitlab.ListMilestonesOptions{ //nolint:exhaustruct
		ListOptions: gitlab.ListOptions{
			PerPage: maxGitLabPageSize,
			Page:    1,
		},
	}
	for {
		page, response, err := client.Milestones.ListMilestones(projectID, options)
		if err != nil {
			errE := errors.WithMessage(err, "failed to list GitLab milestones")
			errors.Details(errE)["page"] = options.Page
			return nil, errE
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

// packageFiles fetches all file names for a packageName/packageID package for GitLab projectID project.
func packageFiles(client *gitlab.Client, projectID, packageName string, packageID int) ([]string, errors.E) {
	files := []string{}
	options := &gitlab.ListPackageFilesOptions{
		PerPage: maxGitLabPageSize,
		Page:    1,
	}
	for {
		page, response, err := client.Packages.ListPackageFiles(projectID, packageID, options)
		if err != nil {
			errE := errors.WithMessage(err, "failed to list GitLab files for package")
			errors.Details(errE)["package"] = packageName
			errors.Details(errE)["page"] = options.Page
			return nil, errE
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

// projectPackages fetches all packages for GitLab projectID project.
func projectPackages(client *gitlab.Client, projectID string) ([]Package, errors.E) {
	packages := []Package{}
	options := &gitlab.ListProjectPackagesOptions{ //nolint:exhaustruct
		ListOptions: gitlab.ListOptions{
			PerPage: maxGitLabPageSize,
			Page:    1,
		},
	}
	for {
		page, response, err := client.Packages.ListProjectPackages(projectID, options)
		if err != nil {
			errE := errors.WithMessage(err, "failed to list GitLab packages")
			errors.Details(errE)["page"] = options.Page
			return nil, errE
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

// projectImages fetches all Docker images for all Docker registries for GitLab projectID project.
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
		page, response, err := client.ContainerRegistry.ListProjectRegistryRepositories(projectID, options)
		if err != nil {
			errE := errors.WithMessage(err, "failed to list GitLab Docker images")
			errors.Details(errE)["page"] = options.Page
			return nil, errE
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

// releaseLinks fetches existing release links for the release for GitLab projectID project.
func releaseLinks(client *gitlab.Client, projectID string, release Release) ([]link, errors.E) {
	links := []link{}
	options := &gitlab.ListReleaseLinksOptions{
		PerPage: maxGitLabPageSize,
		Page:    1,
	}
	for {
		page, response, err := client.ReleaseLinks.ListReleaseLinks(projectID, release.Tag, options)
		if err != nil {
			errE := errors.WithMessage(err, "failed to list GitLab release links for tag")
			errors.Details(errE)["tag"] = release.Tag
			errors.Details(errE)["page"] = options.Page
			return nil, errE
		}

		for _, l := range page {
			l := l

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

type linkOptions = interface {
	gitlab.CreateReleaseLinkOptions | gitlab.ReleaseAssetLinkOptions
}

func createReleaseLinkOptions[T linkOptions](baseURL, projectID, name string, l link) T { //nolint:ireturn
	// TODO: We create one struct and cast it to T for now.
	//       See: https://github.com/golang/go/issues/48522
	options := gitlab.CreateReleaseLinkOptions{ //nolint:exhaustruct
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
			gitlab.PathEscape(projectID),
			gitlab.PathEscape(l.Package.Name),
			gitlab.PathEscape(l.Package.Version),
			gitlab.PathEscape(*l.File),
		)
		options.URL = &url
		options.FilePath = gitlab.String("/" + name)
		options.LinkType = gitlab.LinkType(gitlab.OtherLinkType)
	}
	return T(options)
}

func getExpectedLinks(packages []Package) map[string]link {
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
	return expectedLinks
}

// syncLinks updates release links for the release for GitLab projectID project to match those provided in packages.
//
// For generic packages it makes links to all files for all generic packages. For non-generic packages it makes link
// to each package's web page.
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
	expectedLinks := getExpectedLinks(packages)

	for name, l := range existingLinks {
		_, ok := expectedLinks[name]
		if !ok {
			fmt.Printf("Deleting GitLab link \"%s\" for release \"%s\".\n", l.Name, release.Tag)
			_, _, err := client.ReleaseLinks.DeleteReleaseLink(projectID, release.Tag, *l.ID)
			if err != nil {
				errE := errors.WithMessage(err, "failed to delete GitLab link")
				errors.Details(errE)["link"] = l.Name
				errors.Details(errE)["release"] = release.Tag
				return errE
			}
		}
	}

	for name, l := range expectedLinks {
		existingLink, ok := existingLinks[name]
		if ok {
			fmt.Printf("Updating GitLab link \"%s\" for release \"%s\".\n", l.Name, release.Tag)
			options := &gitlab.UpdateReleaseLinkOptions{ //nolint:exhaustruct
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
					gitlab.PathEscape(projectID),
					gitlab.PathEscape(l.Package.Name),
					gitlab.PathEscape(l.Package.Version),
					gitlab.PathEscape(*l.File),
				)
				options.URL = &url
				options.FilePath = gitlab.String("/" + name)
				options.LinkType = gitlab.LinkType(gitlab.OtherLinkType)
			}
			_, _, err := client.ReleaseLinks.UpdateReleaseLink(projectID, release.Tag, *existingLink.ID, options)
			if err != nil {
				errE := errors.WithMessage(err, "failed to update GitLab link")
				errors.Details(errE)["link"] = l.Name
				errors.Details(errE)["release"] = release.Tag
				return errE
			}
		} else {
			fmt.Printf("Creating GitLab link \"%s\" for release \"%s\".\n", l.Name, release.Tag)
			options := createReleaseLinkOptions[gitlab.CreateReleaseLinkOptions](baseURL, projectID, name, l)
			_, _, err := client.ReleaseLinks.CreateReleaseLink(projectID, release.Tag, &options)
			if err != nil {
				errE := errors.WithMessage(err, "failed to create GitLab link")
				errors.Details(errE)["link"] = l.Name
				errors.Details(errE)["release"] = release.Tag
				return errE
			}
		}
	}

	return nil
}

// Upsert creates or updates a release for the GitLab project given release information,
// milestones associated with the release, packages associated with the release, and
// Docker images associated with the release.
func Upsert(
	config *Config, client *gitlab.Client, release Release, releasedAt *time.Time,
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

	rel, response, err := client.Releases.GetRelease(config.Project, release.Tag)
	if response.StatusCode == http.StatusNotFound {
		if config.NoCreate {
			fmt.Printf("GitLab release for tag \"%s\" is missing, but not creating it per config.\n", release.Tag)
			return nil
		}

		links := []*gitlab.ReleaseAssetLinkOptions{}
		for name, l := range getExpectedLinks(packages) {
			options := createReleaseLinkOptions[gitlab.ReleaseAssetLinkOptions](config.BaseURL, config.Project, name, l)
			links = append(links, &options)
		}

		// Do not provide ReleasedAt field if the release has been done recently.
		// This prevents GitLab from marking the release as a historical release.
		if time.Since(*releasedAt).Abs() < 12*time.Hour {
			releasedAt = nil
		}

		fmt.Printf("Creating GitLab release for tag \"%s\".\n", release.Tag)
		_, _, err = client.Releases.CreateRelease(config.Project, &gitlab.CreateReleaseOptions{
			Name:        &name,
			TagName:     &release.Tag,
			TagMessage:  nil,
			Description: &description,
			Ref:         nil,
			Milestones:  &milestones,
			Assets: &gitlab.ReleaseAssetsOptions{
				Links: links,
			},
			ReleasedAt: releasedAt,
		})
		if err != nil {
			errE := errors.WithMessage(err, "failed to create GitLab release for tag")
			errors.Details(errE)["tag"] = release.Tag
			return errE
		}
		return nil
	} else if err != nil {
		errE := errors.WithMessage(err, "failed to get GitLab release for tag")
		errors.Details(errE)["tag"] = release.Tag
		return errE
	}

	// If GitLab release was made close to releasedAt, we set the releasedAt to CreatedAt
	// to make sure that the release is not marked as a historical release.
	if rel.CreatedAt.Sub(*releasedAt).Abs() < 12*time.Hour {
		releasedAt = rel.CreatedAt
	}

	fmt.Printf("Updating GitLab release for tag \"%s\".\n", release.Tag)
	_, _, err = client.Releases.UpdateRelease(config.Project, release.Tag, &gitlab.UpdateReleaseOptions{
		Name:        &name,
		Description: &description,
		ReleasedAt:  releasedAt,
		Milestones:  &milestones,
	})
	if err != nil {
		errE := errors.WithMessage(err, "failed to update GitLab release for tag")
		errors.Details(errE)["tag"] = release.Tag
		return errE
	}

	return syncLinks(client, config.BaseURL, config.Project, release, packages)
}

// DeleteAllExcept deletes all releases which exist in the GitLab project but
// are not listed in releases.
func DeleteAllExcept(config *Config, client *gitlab.Client, releases []Release) errors.E {
	allReleases := mapset.NewThreadUnsafeSet[string]()
	for _, release := range releases {
		allReleases.Add(release.Tag)
	}

	allGitLabReleases := mapset.NewThreadUnsafeSet[string]()
	options := &gitlab.ListReleasesOptions{ //nolint:exhaustruct
		ListOptions: gitlab.ListOptions{
			PerPage: maxGitLabPageSize,
			Page:    1,
		},
	}
	for {
		page, response, err := client.Releases.ListReleases(config.Project, options)
		if err != nil {
			errE := errors.WithMessage(err, "failed to list GitLab releases")
			errors.Details(errE)["page"] = options.Page
			return errE
		}

		for _, release := range page {
			allGitLabReleases.Add(release.TagName)
		}

		if response.NextPage == 0 {
			break
		}

		options.Page = response.NextPage
	}

	extraGitLabReleases := allGitLabReleases.Difference(allReleases).ToSlice()
	slices.Sort(extraGitLabReleases)
	for _, tag := range extraGitLabReleases {
		fmt.Printf("Deleting GitLab release for tag \"%s\".\n", tag)
		_, _, err := client.Releases.DeleteRelease(config.Project, tag)
		if err != nil {
			errE := errors.WithMessage(err, "failed to delete GitLab release for tag")
			errors.Details(errE)["tag"] = tag
			return errE
		}
	}

	return nil
}

// noChange is an identify function for strings.
func noChange(s string) string {
	return s
}

// removeVPrefix removes "v" from the beginning of the string.
func removeVPrefix(s string) string {
	return strings.TrimPrefix(s, "v")
}

// slugify makes a slug from the string, matching what is used in GitLab.
// See: https://gitlab.com/gitlab-org/gitlab/-/blob/c61e4166/lib/gitlab/utils.rb#L73-84
func slugify(s string) string {
	return refSlug(s)
}

// removeVPrefixAndSlugify combines removeVPrefix and refSlug.
func removeVPrefixAndSlugify(s string) string {
	return refSlug(removeVPrefix(s))
}

var tagTransformations = []func(string) string{noChange, removeVPrefix, slugify, removeVPrefixAndSlugify} //nolint:gochecknoglobals

// mapStringsToTags attempts to map input strings to releases' tags by searching for
// each release's tag (i.e., version with "v" prefix) or version (i.e., tag without
// "v" prefix) in strings and those which match are associated with the tag/version.
//
// It starts with the longest tags so that more specific tags are mapped first.
// This makes string "1.0.0-rc" be mapped to tag "1.0.0-rc" if such a tag exist
// together with the "1.0.0" tag. On the other hand, if only "1.0.0" tag exists,
// then "1.0.0-rc" is mapped to "1.0.0".
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

	assignedInputs := mapset.NewThreadUnsafeSet[string]()
	for _, transformation := range tagTransformations {
		for _, tag := range tags {
			t := transformation(tag)

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

// mapMilestonesToTags maps provided milestones to releases' tags.
func mapMilestonesToTags(milestones []string, releases []Release) map[string][]string {
	return mapStringsToTags(milestones, releases)
}

// mapMilestonesToTags maps provided packages to releases' tags.
//
// Packages are mapped based on their version string.
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

	assignedPackages := mapset.NewThreadUnsafeSet[int]()
	for _, transformation := range tagTransformations {
		for _, tag := range tags {
			t := transformation(tag)

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

// mapMilestonesToTags maps provided Docker images to releases' tags.
func mapImagesToTags(images []string, releases []Release) map[string][]string {
	return mapStringsToTags(images, releases)
}

func mapTagsToDates(tags []Tag) map[string]*time.Time {
	tagsToDates := map[string]*time.Time{}
	for _, tag := range tags {
		tag := tag
		tagsToDates[tag.Name] = &tag.Date
	}
	return tagsToDates
}

// Sync syncs tags in a git repository and a changelog in Keep a Changelog format with
// releases of a GitLab project. It creates any missing release, it updates existing
// releases, and it deletes and releases which do not exist anymore.
func Sync(config *Config) errors.E {
	releases, errE := changelogReleases(config.Changelog)
	if errE != nil {
		return errE
	}

	tags, errE := gitTags(".")
	if errE != nil {
		return errE
	}

	errE = compareReleasesTags(releases, tags)
	if errE != nil {
		return errE
	}

	if config.Project == "" {
		projectID, errE := x.InferGitLabProjectID(".") //nolint:govet
		if errE != nil {
			return errE
		}
		config.Project = projectID
	}

	client, err := gitlab.NewClient(config.Token, gitlab.WithBaseURL(config.BaseURL))
	if err != nil {
		return errors.WithMessage(err, "failed to create GitLab API client instance")
	}

	hasIssues, hasPackages, hasImages, errE := projectConfiguration(client, config.Project)
	if errE != nil {
		return errE
	}

	tagsToMilestones := map[string][]string{}
	if hasIssues {
		milestones, errE := projectMilestones(client, config.Project) //nolint:govet
		if errE != nil {
			return errE
		}

		tagsToMilestones = mapMilestonesToTags(milestones, releases)
	}

	tagsToPackages := map[string][]Package{}
	if hasPackages {
		packages, errE := projectPackages(client, config.Project) //nolint:govet
		if errE != nil {
			return errE
		}

		tagsToPackages = mapPackagesToTags(packages, releases)
	}

	tagsToImages := map[string][]string{}
	if hasImages {
		images, errE := projectImages(client, config.Project) //nolint:govet
		if errE != nil {
			return errE
		}

		tagsToImages = mapImagesToTags(images, releases)
	}

	tagsToDates := mapTagsToDates(tags)

	for _, release := range releases {
		errE = Upsert(
			config, client, release, tagsToDates[release.Tag],
			tagsToMilestones[release.Tag], tagsToPackages[release.Tag], tagsToImages[release.Tag],
		)
		if errE != nil {
			return errE
		}
	}

	errE = DeleteAllExcept(config, client, releases)
	if errE != nil {
		return errE
	}

	return nil
}
