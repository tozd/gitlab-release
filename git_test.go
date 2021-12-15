package release

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferProjectID(t *testing.T) {
	tests := []struct {
		remote    string
		projectID string
	}{
		{"https://gitlab.com/tozd/gitlab/release.git", "tozd/gitlab/release"},
		{"git@gitlab.com:tozd/gitlab/release.git", "tozd/gitlab/release"},
	}

	for k, tt := range tests {
		t.Run(fmt.Sprintf("case=%d", k), func(t *testing.T) {
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
			err = os.WriteFile(filename, []byte("Hello world!"), 0o600)
			require.NoError(t, err)
			_, err = workTree.Add("file.txt")
			require.NoError(t, err)
			_, err = workTree.Commit("Initial commmit.", &git.CommitOptions{
				Author: author,
			})
			require.NoError(t, err)
			_, err = repository.CreateRemote(&config.RemoteConfig{
				Name: "origin",
				URLs: []string{tt.remote},
			})
			require.NoError(t, err)
			projectID, err := inferProjectID(tempDir)
			require.NoError(t, err)
			assert.Equal(t, tt.projectID, projectID)
		})
	}
}
