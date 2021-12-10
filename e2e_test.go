package release

import (
	"os"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestE2E(t *testing.T) {
	if os.Getenv("GITLAB_API_TOKEN") == "" {
		t.Skip("GITLAB_API_TOKEN is not available")
	}

	var config Config
	parser, err := kong.New(&config, kong.Exit(func(_ int) {}))
	require.NoError(t, err)

	_, err = parser.Parse([]string{})
	require.NoError(t, err)

	err = Sync(&config)
	require.NoError(t, err)
}
