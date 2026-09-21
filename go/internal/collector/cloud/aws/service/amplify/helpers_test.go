// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amplify

import "testing"

func TestSanitizeRepositoryURLStripsToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "https token userinfo is dropped",
			in:   "https://x-access-token:ghp_SECRET@github.com/acme/storefront.git",
			want: "https://github.com/acme/storefront.git",
		},
		{
			name: "plain https url is host and path",
			in:   "https://github.com/acme/storefront",
			want: "https://github.com/acme/storefront",
		},
		{
			name: "query and fragment are dropped",
			in:   "https://github.com/acme/storefront?token=abc#frag",
			want: "https://github.com/acme/storefront",
		},
		{
			name: "scp-style git address drops userinfo",
			in:   "git@github.com:acme/storefront.git",
			want: "github.com:acme/storefront.git",
		},
		{
			name: "empty stays empty",
			in:   "   ",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeRepositoryURL(tc.in); got != tc.want {
				t.Fatalf("SanitizeRepositoryURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCloudFrontDomainFromDNSRecord(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "cname with type token", in: "CNAME d2example.cloudfront.net", want: "d2example.cloudfront.net"},
		{name: "trailing dot trimmed", in: "d2example.cloudfront.net.", want: "d2example.cloudfront.net"},
		{name: "bare value", in: "d2example.cloudfront.net", want: "d2example.cloudfront.net"},
		{name: "non-cloudfront record yields empty", in: "CNAME example.s3.amazonaws.com", want: ""},
		{name: "empty yields empty", in: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cloudFrontDomainFromDNSRecord(tc.in); got != tc.want {
				t.Fatalf("cloudFrontDomainFromDNSRecord(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
