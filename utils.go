package release

import (
	"net/url"
	"regexp"
	"strings"
)

const (
	slugMaxLength = 63
)

var (
	slugCleanupRegex *regexp.Regexp
	slugTrimRegex    *regexp.Regexp
)

func init() {
	slugCleanupRegex = regexp.MustCompile(`[^a-z0-9]`)
	slugTrimRegex = regexp.MustCompile(`(\A-+|-+\z)`)
}

// join is the same as strings.Join, only that it takes a slice of interface{}
// as input.
func join(elems []interface{}, sep string) string {
	switch len(elems) {
	case 0:
		return ""
	case 1:
		return elems[0].(string)
	}
	n := len(sep) * (len(elems) - 1)
	for i := 0; i < len(elems); i++ {
		n += len(elems[i].(string))
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(elems[0].(string))
	for _, s := range elems[1:] {
		b.WriteString(sep)
		b.WriteString(s.(string))
	}
	return b.String()
}

// pathEscape is a helper function to escape a project identifier.
func pathEscape(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ".", "%2E")
}

func refSlug(s string) string {
	s = strings.ToLower(s)
	s = slugCleanupRegex.ReplaceAllString(s, "-")
	if len(s) > slugMaxLength {
		s = s[:slugMaxLength]
	}
	s = slugTrimRegex.ReplaceAllString(s, "")
	return s
}
