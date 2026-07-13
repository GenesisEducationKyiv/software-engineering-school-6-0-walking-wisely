package subscriptionapp

import "testing"

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", false},
		{"no at-sign", "notanemail", false},
		{"multiple at-signs", "a@b@c.com", false},
		{"empty local part", "@subscriptions.com", false},
		{"domain without dot", "user@domain", false},
		{"empty domain", "user@", false},
		{"valid", "user@example.com", true},
		{"subdomain", "user@sub.example.com", true},
		{"dot in local part", "first.last@example.com", true},
		{"plus in local part", "user+tag@example.com", true},
		// Quirk: implementation only checks for presence of a dot, not TLD validity.
		{"trailing dot in domain", "user@example.", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValidEmail(tc.input); got != tc.want {
				t.Errorf("IsValidEmail(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsValidToken(t *testing.T) {
	// 64-char valid lowercase hex string used as a base.
	const valid = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", false},
		{"63 valid hex chars", valid[:63], false},
		{"65 valid hex chars", valid + "a", false},
		{"64 lowercase hex", valid, true},
		{"all zeros", "0000000000000000000000000000000000000000000000000000000000000000", true},
		{"all fs", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", true},
		{"uppercase hex char", "A" + valid[1:], false},
		{"non-hex letter g", "g" + valid[1:], false},
		{"space character", " " + valid[1:], false},
		{"hyphen character", "-" + valid[1:], false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValidToken(tc.input); got != tc.want {
				t.Errorf("IsValidToken(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
