// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querydecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestAdvisoryDecodeWrappersClassifyMissingRequiredField proves the same
// accuracy guarantee the root table
// (TestSupplyChainDecodeWrappersClassifyMissingRequiredField) proves for the
// wrappers that stayed behind: a required payload key absent (or null) from
// a source-fact payload must dead-letter as a classified input_invalid
// *querydecode.Error, never a silent zero-value struct. These four
// vulnerability cases moved here with the wrappers (#6060 lane A) so the
// root table keeps covering only the wrappers still living in root package
// query.
func TestAdvisoryDecodeWrappersClassifyMissingRequiredField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		decode       func(supplyChainFactDecodeInput) error
		payload      map[string]any
		wantFactKind string
		missingField string
	}{
		{
			name: "vulnerability.cve missing advisory_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeVulnerabilityCVE(in)
				return err
			},
			payload:      map[string]any{"cve_id": "CVE-2026-0001"},
			wantFactKind: factschema.FactKindVulnerabilityCVE,
			missingField: "advisory_id",
		},
		{
			name: "vulnerability.affected_package missing advisory_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeVulnerabilityAffectedPackage(in)
				return err
			},
			payload:      map[string]any{"cve_id": "CVE-2026-0001", "ecosystem": "npm"},
			wantFactKind: factschema.FactKindVulnerabilityAffectedPackage,
			missingField: "advisory_id",
		},
		{
			name: "vulnerability.epss_score missing cve_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeVulnerabilityEPSSScore(in)
				return err
			},
			payload:      map[string]any{"probability": "0.5"},
			wantFactKind: factschema.FactKindVulnerabilityEPSSScore,
			missingField: "cve_id",
		},
		{
			name: "vulnerability.known_exploited missing cve_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeVulnerabilityKnownExploited(in)
				return err
			},
			payload:      map[string]any{"vendor_project": "Example Corp"},
			wantFactKind: factschema.FactKindVulnerabilityKnownExploited,
			missingField: "cve_id",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.decode(supplyChainFactDecodeInput{FactID: "fact-1", Payload: tc.payload})
			if err == nil {
				t.Fatalf("decode error = nil, want classified input_invalid error for missing %q", tc.missingField)
			}
			var decodeErr *querydecode.Error
			if !errors.As(err, &decodeErr) {
				t.Fatalf("error = %v (%T), want *querydecode.Error", err, err)
			}
			if decodeErr.Classification != factschema.ClassificationInputInvalid {
				t.Fatalf("Classification = %q, want %q", decodeErr.Classification, factschema.ClassificationInputInvalid)
			}
			if decodeErr.FactKind != tc.wantFactKind {
				t.Fatalf("FactKind = %q, want %q", decodeErr.FactKind, tc.wantFactKind)
			}
			if decodeErr.FactID != "fact-1" {
				t.Fatalf("FactID = %q, want %q", decodeErr.FactID, "fact-1")
			}
			if decodeErr.Field != tc.missingField {
				t.Fatalf("Field = %q, want %q", decodeErr.Field, tc.missingField)
			}
		})
	}
}
