package release

import (
	"strings"
)

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
