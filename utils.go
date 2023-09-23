package release

import (
	"regexp"
	"strings"
)

const (
	slugMaxLength = 63
)

var (
	slugCleanupRegex = regexp.MustCompile(`[^a-z0-9]`)
	slugTrimRegex    = regexp.MustCompile(`(\A-+|-+\z)`)
)

func refSlug(s string) string {
	s = strings.ToLower(s)
	s = slugCleanupRegex.ReplaceAllString(s, "-")
	if len(s) > slugMaxLength {
		s = s[:slugMaxLength]
	}
	s = slugTrimRegex.ReplaceAllString(s, "")
	return s
}
