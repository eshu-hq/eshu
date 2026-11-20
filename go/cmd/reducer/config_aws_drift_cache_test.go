// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
)

// TestAWSDriftEvidenceCacheMaxBytesDefaults pins the production cache
// budget: unset, zero, negative, or non-numeric input keeps the
// evidence-based default so a typo cannot disable sharing or blow the
// budget; a positive value overrides.
func TestAWSDriftEvidenceCacheMaxBytesDefaults(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want int64
	}{
		{name: "empty uses default", raw: "", want: awscloud.DefaultAWSDriftEvidenceCacheMaxBytes},
		{name: "zero uses default", raw: "0", want: awscloud.DefaultAWSDriftEvidenceCacheMaxBytes},
		{name: "negative uses default", raw: "-1", want: awscloud.DefaultAWSDriftEvidenceCacheMaxBytes},
		{name: "non-numeric uses default", raw: "abc", want: awscloud.DefaultAWSDriftEvidenceCacheMaxBytes},
		{name: "positive overrides", raw: "134217728", want: 134217728},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := awsDriftEvidenceCacheMaxBytes(func(string) string { return tc.raw }, nil)
			if got != tc.want {
				t.Fatalf("awsDriftEvidenceCacheMaxBytes(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}
