package release

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRefSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"master", "master"},
		{"v1.0.0", "v1-0-0"},
		{"1-foo", "1-foo"},
		{"fix/1-foo", "fix-1-foo"},
		{"fix-1-foo", "fix-1-foo"},
		{strings.Repeat("a", 63), strings.Repeat("a", 63)},
		{strings.Repeat("a", 64), strings.Repeat("a", 63)},
		{"FOO", "foo"},
		{"-" + strings.Repeat("a", 61) + "-", strings.Repeat("a", 61)},
		{"-" + strings.Repeat("a", 62) + "-", strings.Repeat("a", 62)},
		{"-" + strings.Repeat("a", 63) + "-", strings.Repeat("a", 62)},
		{strings.Repeat("a", 62) + " ", strings.Repeat("a", 62)},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(fmt.Sprintf("case=%s", tt.input), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, refSlug(tt.input))
		})
	}
}
