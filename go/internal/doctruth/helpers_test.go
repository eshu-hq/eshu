package doctruth

import "testing"

func TestContainsTokenUsesBoundariesWithoutRegexp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		text  string
		token string
		want  bool
	}{
		{
			name:  "exact token",
			text:  "payment-api deploys through Helm",
			token: "payment-api",
			want:  true,
		},
		{
			name:  "case insensitive",
			text:  "Payment-API deploys through Helm",
			token: "payment-api",
			want:  true,
		},
		{
			name:  "does not match embedded token",
			text:  "payment-api-worker deploys through Helm",
			token: "payment-api",
			want:  false,
		},
		{
			name:  "matches code path",
			text:  "entrypoint is services/payment-worker/main.go",
			token: "services/payment-worker/main.go",
			want:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := containsToken(tt.text, tt.token); got != tt.want {
				t.Fatalf("containsToken(%q, %q) = %t, want %t", tt.text, tt.token, got, tt.want)
			}
		})
	}
}
