package subscriptionapp

import "strings"

// NormalizeEmail converts user input into the canonical subscription email form.
func NormalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

// NormalizeRepo removes transport/input whitespace around a GitHub repo name.
func NormalizeRepo(repo string) string {
	return strings.TrimSpace(repo)
}

// IsValidEmail is a lightweight sanity check. It is not RFC 5321 complete, but
// rejects obvious garbage before external calls or persistence.
func IsValidEmail(email string) bool {
	parts := strings.Split(email, "@")
	return len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".")
}

// IsValidRepo checks the "owner/repo" GitHub repository format accepted by the service.
func IsValidRepo(repo string) bool {
	return repoPattern.MatchString(repo)
}

// IsValidToken checks that the string is a 64-character lowercase hex string,
// which is the output format of our HMAC-SHA256 token generator.
func IsValidToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, c := range token {
		//nolint:staticcheck // De Morgan's law makes this less readable here
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
