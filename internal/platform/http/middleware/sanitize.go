package middleware

import (
	"strings"
	"unicode"
)

// sanitizeLogValue strips control characters to mitigate log injection.
func sanitizeLogValue(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}
