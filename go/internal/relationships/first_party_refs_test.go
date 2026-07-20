// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import "testing"

// TestExtractTerraformRefPin covers the query-parameter extraction table for
// the #5441 edge property first_party_ref_version: a Terraform/Terragrunt
// module source pin (the `ref=` query parameter on a go-getter style source
// string) that was previously computed but never carried onto the graph edge.
func TestExtractTerraformRefPin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "no query string",
			raw:  "git::https://example.test/org/mod.git",
			want: "",
		},
		{
			name: "ref only",
			raw:  "git::https://example.test/org/mod.git?ref=v1.2.3",
			want: "v1.2.3",
		},
		{
			name: "ref among other query params",
			raw:  "git::https://example.test/org/mod.git?depth=1&ref=v1.2.3",
			want: "v1.2.3",
		},
		{
			name: "ref before other query params",
			raw:  "git::https://example.test/org/mod.git?ref=v1.2.3&depth=1",
			want: "v1.2.3",
		},
		{
			name: "empty ref value",
			raw:  "git::https://example.test/org/mod.git?ref=",
			want: "",
		},
		{
			name: "query string without ref",
			raw:  "git::https://example.test/org/mod.git?depth=1",
			want: "",
		},
		{
			name: "empty source",
			raw:  "",
			want: "",
		},
		{
			name: "whitespace only source",
			raw:  "   ",
			want: "",
		},
		{
			name: "registry source with ref-like version suffix is not a query ref",
			raw:  "terraform-aws-modules/vpc/aws",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExtractTerraformRefPin(tt.raw); got != tt.want {
				t.Fatalf("ExtractTerraformRefPin(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestExtractTerraformRefPinDoesNotChangeNormalizeTerraformFirstPartyRef
// guards the explicit design constraint that the new pin-extraction helper
// must not alter normalizeTerraformFirstPartyRef's existing stripped-ref
// output, which has pinned consumers (module_name/source_ref matching).
func TestExtractTerraformRefPinDoesNotChangeNormalizeTerraformFirstPartyRef(t *testing.T) {
	t.Parallel()

	raw := "git::https://example.test/org/mod.git?ref=v1.2.3"
	wantNormalized := "https://example.test/org/mod.git"
	if got := normalizeTerraformFirstPartyRef(raw); got != wantNormalized {
		t.Fatalf("normalizeTerraformFirstPartyRef(%q) = %q, want %q (must stay unchanged)", raw, got, wantNormalized)
	}
	if got, want := ExtractTerraformRefPin(raw), "v1.2.3"; got != want {
		t.Fatalf("ExtractTerraformRefPin(%q) = %q, want %q", raw, got, want)
	}
}
