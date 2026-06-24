// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCycloneDXLicensesAreSorted proves the projected license slice is
// independent of input ordering. Producers commonly emit license arrays in
// non-deterministic order; the fact bundle must still be byte-identical.
func TestCycloneDXLicensesAreSorted(t *testing.T) {
	t.Parallel()

	asc := []cycloneDXLicense{
		{License: &cycloneDXLicenseDetail{ID: "Apache-2.0"}},
		{License: &cycloneDXLicenseDetail{ID: "MIT"}},
		{Expression: "BSD-3-Clause"},
	}
	desc := []cycloneDXLicense{
		{Expression: "BSD-3-Clause"},
		{License: &cycloneDXLicenseDetail{ID: "MIT"}},
		{License: &cycloneDXLicenseDetail{ID: "Apache-2.0"}},
	}
	if got, want := cycloneDXLicenses(asc), cycloneDXLicenses(desc); !reflect.DeepEqual(got, want) {
		t.Fatalf("cycloneDXLicenses ordering differs:\n  asc=%#v\n  desc=%#v", got, want)
	}
}

// TestSPDXLicensesAreSorted proves SPDX license projection is stable when
// declared/concluded are swapped or when license-from-files appears first.
func TestSPDXLicensesAreSorted(t *testing.T) {
	t.Parallel()

	a := spdxPackage{
		LicenseDeclared:      "MIT",
		LicenseConcluded:     "Apache-2.0",
		LicenseInfoFromFiles: []string{"BSD-3-Clause", "ISC"},
	}
	b := spdxPackage{
		LicenseDeclared:      "Apache-2.0",
		LicenseConcluded:     "MIT",
		LicenseInfoFromFiles: []string{"ISC", "BSD-3-Clause"},
	}
	if got, want := spdxLicenses(a), spdxLicenses(b); !reflect.DeepEqual(got, want) {
		t.Fatalf("spdxLicenses ordering differs:\n  a=%#v\n  b=%#v", got, want)
	}
}

// TestExternalRefEnvelopesAreSorted proves external reference projection is
// independent of input ordering for both CycloneDX and SPDX.
func TestExternalRefEnvelopesAreSorted(t *testing.T) {
	t.Parallel()

	ctx := validFixtureContext()
	docID := "doc-1"
	componentID := "component-1"

	asc := []cycloneDXExternalRef{
		{Type: "vcs", URL: "https://example.com/a"},
		{Type: "website", URL: "https://example.com/b"},
		{Type: "issue-tracker", URL: "https://example.com/c"},
	}
	desc := []cycloneDXExternalRef{
		{Type: "issue-tracker", URL: "https://example.com/c"},
		{Type: "website", URL: "https://example.com/b"},
		{Type: "vcs", URL: "https://example.com/a"},
	}
	got := envelopeKeys(cycloneDXExternalRefEnvelopes(ctx, docID, componentID, asc))
	want := envelopeKeys(cycloneDXExternalRefEnvelopes(ctx, docID, componentID, desc))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cyclonedx external ref ordering differs:\n  got=%v\n  want=%v", got, want)
	}

	spdxAsc := []spdxExternalRef{
		{ReferenceType: "purl", ReferenceLocator: "pkg:npm/a@1"},
		{ReferenceType: "cpe23Type", ReferenceLocator: "cpe:b"},
	}
	spdxDesc := []spdxExternalRef{
		{ReferenceType: "cpe23Type", ReferenceLocator: "cpe:b"},
		{ReferenceType: "purl", ReferenceLocator: "pkg:npm/a@1"},
	}
	gotSPDX := envelopeKeys(spdxExternalRefEnvelopes(ctx, docID, componentID, spdxAsc))
	wantSPDX := envelopeKeys(spdxExternalRefEnvelopes(ctx, docID, componentID, spdxDesc))
	if !reflect.DeepEqual(gotSPDX, wantSPDX) {
		t.Fatalf("spdx external ref ordering differs:\n  got=%v\n  want=%v", gotSPDX, wantSPDX)
	}
}

// TestCycloneDXMetadataToolHandlesMissingFields proves missing tool name or
// version fields never pollute the document fact with the literal string
// "<nil>".
func TestCycloneDXMetadataToolHandlesMissingFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		doc  cycloneDXDocument
		want string
	}{
		{
			name: "name only",
			doc: cycloneDXDocument{Metadata: &cycloneDXMetadata{
				Tools: []any{map[string]any{"name": "syft"}},
			}},
			want: "syft",
		},
		{
			name: "version only",
			doc: cycloneDXDocument{Metadata: &cycloneDXMetadata{
				Tools: []any{map[string]any{"version": "1.0"}},
			}},
			want: "1.0",
		},
		{
			name: "both missing",
			doc: cycloneDXDocument{Metadata: &cycloneDXMetadata{
				Tools: []any{map[string]any{}},
			}},
			want: "",
		},
		{
			name: "components shape",
			doc: cycloneDXDocument{Metadata: &cycloneDXMetadata{
				Tools: map[string]any{"components": []any{map[string]any{"name": "syft"}}},
			}},
			want: "syft",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cycloneDXMetadataTool(tc.doc); got != tc.want {
				t.Fatalf("cycloneDXMetadataTool(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func envelopeKeys(envelopes []facts.Envelope) []string {
	out := make([]string, 0, len(envelopes))
	for _, e := range envelopes {
		out = append(out, e.StableFactKey)
	}
	return out
}
