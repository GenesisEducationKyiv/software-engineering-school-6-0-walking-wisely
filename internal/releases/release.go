// Package releases defines provider-neutral release data used by subscription workflows.
package releases

// Release is the release information needed to notify subscribers.
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Name    string `json:"name"`
}
