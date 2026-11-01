// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"reflect"
	"testing"
)

// TestDecodeFindingRowKeepsTheAdvisoryRetraction proves a retracted finding
// reaches the caller with the reason and replacement the reducer wrote (#9695).
// The read path decodes provenance into a typed struct, so a key the struct
// does not name is dropped silently; this pins the new keys.
func TestDecodeFindingRowKeepsTheAdvisoryRetraction(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
        "cve_id": "CVE-2026-4001",
        "advisory_id": "CVE-2026-4001",
        "package_id": "npm://registry.npmjs.org/widget",
        "impact_status": "affected_exact",
        "suppression_state": "advisory_retracted",
        "suppression": {
            "state": "advisory_retracted",
            "source": "advisory_retraction",
            "reason": "advisory retracted by its source: nvd rejected CVE-2026-4001; replaced by CVE-2026-4002"
        },
        "provenance": {
            "advisory_sources": [
                {"source": "nvd", "advisory_id": "CVE-2026-4001", "status": "Rejected", "replaced_by": ["CVE-2026-4002"]}
            ],
            "advisory_retraction": {
                "reason_code": "rejected",
                "reason": "advisory retracted by its source: nvd rejected CVE-2026-4001; replaced by CVE-2026-4002",
                "replaced_by": ["CVE-2026-4002"],
                "sources": [
                    {"source": "nvd", "advisory_id": "CVE-2026-4001", "reason_code": "rejected", "status": "Rejected", "replaced_by": ["CVE-2026-4002"]}
                ]
            }
        }
    }`)

	row, err := DecodeFindingRow("finding-4001", "inferred", payload)
	if err != nil {
		t.Fatalf("DecodeFindingRow() error = %v", err)
	}
	if row.Provenance == nil {
		t.Fatal("Provenance = nil, want the decoded block")
	}
	if got := row.Suppression; got == nil || got.State != "advisory_retracted" || got.Source != "advisory_retraction" {
		t.Fatalf("Suppression = %#v, want the advisory_retracted decision", got)
	}
	retraction := row.Provenance.AdvisoryRetraction
	if retraction == nil {
		t.Fatalf("AdvisoryRetraction = nil, want it decoded from provenance: %#v", row.Provenance)
	}
	if retraction.ReasonCode != "rejected" || retraction.Reason == "" {
		t.Fatalf("retraction = %#v, want reason_code rejected and a reason", retraction)
	}
	if want := []string{"CVE-2026-4002"}; !reflect.DeepEqual(retraction.ReplacedBy, want) {
		t.Fatalf("ReplacedBy = %v, want %v", retraction.ReplacedBy, want)
	}
	if len(retraction.Sources) != 1 || retraction.Sources[0].Status != "Rejected" ||
		!reflect.DeepEqual(retraction.Sources[0].ReplacedBy, []string{"CVE-2026-4002"}) {
		t.Fatalf("Sources = %#v, want the nvd rejection", retraction.Sources)
	}
	source := row.Provenance.AdvisorySources[0]
	if source.Status != "Rejected" || !reflect.DeepEqual(source.ReplacedBy, []string{"CVE-2026-4002"}) {
		t.Fatalf("AdvisorySources[0] = %#v, want status and replaced_by kept", source)
	}
}

// TestDecodeFindingRowWithoutRetractionHasNone proves a finding whose advisory
// stands carries no retraction block.
func TestDecodeFindingRowWithoutRetractionHasNone(t *testing.T) {
	t.Parallel()

	row, err := DecodeFindingRow("finding-4003", "inferred", []byte(`{
        "cve_id": "CVE-2026-4003", "advisory_id": "CVE-2026-4003", "impact_status": "affected_exact",
        "provenance": {"advisory_sources": [{"source": "nvd", "advisory_id": "CVE-2026-4003"}]}
    }`))
	if err != nil {
		t.Fatalf("DecodeFindingRow() error = %v", err)
	}
	if row.Provenance == nil || row.Provenance.AdvisoryRetraction != nil {
		t.Fatalf("Provenance = %#v, want no retraction", row.Provenance)
	}
}
