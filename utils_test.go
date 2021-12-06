package release

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJoin(t *testing.T) {
	tests := []struct {
		elems []string
		sep   string
	}{
		{[]string{"a", "b"}, ""},
		{[]string{"a", "b"}, ","},
		{[]string{}, ","},
		{[]string{}, ""},
	}

	for k, tt := range tests {
		t.Run(fmt.Sprintf("case=%d", k), func(t *testing.T) {
			input := make([]interface{}, len(tt.elems))
			for i, e := range tt.elems {
				input[i] = e
			}
			assert.Equal(t, strings.Join(tt.elems, tt.sep), join(input, tt.sep))
		})
	}
}

func TestPathEscape(t *testing.T) {
	assert.Equal(t, "diaspora%2Fdiaspora", pathEscape("diaspora/diaspora"))
}

func TestRefSlug(t *testing.T) {
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
		t.Run(fmt.Sprintf("case=%s", tt.input), func(t *testing.T) {
			assert.Equal(t, tt.want, refSlug(tt.input))
		})
	}
}
