// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import "testing"

func TestDeriveMaturity(t *testing.T) {
	t.Parallel()

	const (
		ll = "local_lightweight"
		la = "local_authoritative"
		fs = "local_full_stack"
		pr = "production"
	)

	cases := []struct {
		name     string
		profiles map[string]ProfileSupport
		want     Maturity
	}{
		{
			name: "production supported is general availability",
			profiles: map[string]ProfileSupport{
				ll: {Status: "unsupported"},
				la: {Status: "supported"},
				fs: {Status: "supported"},
				pr: {Status: "supported"},
			},
			want: MaturityGeneralAvailability,
		},
		{
			name: "production experimental is experimental",
			profiles: map[string]ProfileSupport{
				la: {Status: "supported"},
				pr: {Status: "experimental"},
			},
			want: MaturityExperimental,
		},
		{
			name: "production unsupported but local supported is preview",
			profiles: map[string]ProfileSupport{
				la: {Status: "supported"},
				fs: {Status: "supported"},
				pr: {Status: "unsupported"},
			},
			want: MaturityPreview,
		},
		{
			name: "all unsupported is not implemented",
			profiles: map[string]ProfileSupport{
				ll: {Status: "unsupported"},
				la: {Status: "unsupported"},
				fs: {Status: "unsupported"},
				pr: {Status: "unsupported"},
			},
			want: MaturityNotImplemented,
		},
		{
			name:     "empty profiles is not implemented",
			profiles: map[string]ProfileSupport{},
			want:     MaturityNotImplemented,
		},
		{
			name: "no production row falls back to best local status",
			profiles: map[string]ProfileSupport{
				ll: {Status: "supported"},
			},
			want: MaturityPreview,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deriveMaturity(tc.profiles); got != tc.want {
				t.Fatalf("deriveMaturity() = %q, want %q", got, tc.want)
			}
		})
	}
}
