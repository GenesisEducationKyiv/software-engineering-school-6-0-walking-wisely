package middleware

import "testing"

func TestSanitizeLogValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "leaves printable ASCII unchanged",
			value: "/subscriptions/123",
			want:  "/subscriptions/123",
		},
		{
			name:  "removes CRLF used for log injection",
			value: "/login\r\nlevel=error msg=forged",
			want:  "/loginlevel=error msg=forged",
		},
		{
			name:  "removes common ASCII control characters",
			value: "GET\t/admin\x00hidden",
			want:  "GET/adminhidden",
		},
		{
			name:  "removes ANSI escape introducer",
			value: "\x1b[31m/error\x1b[0m",
			want:  "[31m/error[0m",
		},
		{
			name:  "removes DEL",
			value: "/users\x7f/42",
			want:  "/users/42",
		},
		{
			name:  "removes Unicode C1 controls",
			value: "/users\u0085/42\u009bstatus",
			want:  "/users/42status",
		},
		{
			name:  "leaves non-ASCII printable runes unchanged",
			value: "/привіт/✅",
			want:  "/привіт/✅",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeLogValue(tt.value); got != tt.want {
				t.Fatalf("sanitizeLogValue(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
