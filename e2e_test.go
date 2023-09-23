package release

import (
	"os"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestE2E(t *testing.T) {
	t.Parallel()

	if os.Getenv("GITLAB_API_TOKEN") == "" {
		t.Skip("GITLAB_API_TOKEN is not available")
	}

	var config Config
	parser, err := kong.New(&config, kong.Exit(func(code int) {
		if code != 0 {
			t.Errorf("Kong exited with code %d", code)
		}
	}))
	require.NoError(t, err)

	_, err = parser.Parse([]string{})
	require.NoError(t, err)

	err = Sync(&config)
	require.NoError(t, err, "% -+#.1v", err)
}
