package contracts

// Release is the release information shared between GitHub integration,
// release monitoring, and notification events.
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Name    string `json:"name"`
}
