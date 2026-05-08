package middleware

import "strings"

// sanitizeLogValue strips control characters to mitigate log injection.
func sanitizeLogValue(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
}
