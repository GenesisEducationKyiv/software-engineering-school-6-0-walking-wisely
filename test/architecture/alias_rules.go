package architecture

// AliasAllowRule defines an exception for an import alias that would otherwise
// be flagged as unnecessary. If Alias is empty, all aliases for PkgPath are allowed.
type AliasAllowRule struct {
	PkgPath string
	Alias   string
}

// AliasAllowRules lists exceptions where import aliases are permitted even
// when the alias is not strictly needed for disambiguation in the file.
var AliasAllowRules []AliasAllowRule
