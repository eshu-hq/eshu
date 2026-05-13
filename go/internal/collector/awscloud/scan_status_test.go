package awscloud

import (
	"strings"
	"testing"
)

func TestSanitizeScanStatusMessageRedactsAndBounds(t *testing.T) {
	t.Parallel()

	message := strings.Repeat("prefix ", 60) +
		"arn:aws:sts::123456789012:assumed-role/Admin/session " +
		"request 7bb9d111-4c2c-4f8b-9b7a-72f1aa2a0c55\nAKIAABCDEFGHIJKLMNOP"
	got := SanitizeScanStatusMessage(message)

	for _, forbidden := range []string{
		"123456789012",
		"arn:aws",
		"7bb9d111-4c2c-4f8b-9b7a-72f1aa2a0c55",
		"AKIAABCDEFGHIJKLMNOP",
		"\n",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("SanitizeScanStatusMessage() = %q, contains %q", got, forbidden)
		}
	}
	if len(got) > MaxScanStatusMessageLength {
		t.Fatalf("SanitizeScanStatusMessage() length = %d, want <= %d", len(got), MaxScanStatusMessageLength)
	}
}
