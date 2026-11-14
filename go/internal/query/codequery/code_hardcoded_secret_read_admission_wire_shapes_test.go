// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// codeHardcodedSecretAdmissionCannedStore is the pin-satisfying double
// behind the wire-shape proof: it implements exactly the two pinned
// secret reads over fixed canned rows and is compile-asserted against
// the pin, so the proof below runs through the admission shape, not the
// wider staying ports.
type codeHardcodedSecretAdmissionCannedStore struct {
	findings []HardcodedSecretFindingRow
	source   HardcodedSecretReadSource
}

func codeHardcodedSecretAdmissionCannedFindings() []HardcodedSecretFindingRow {
	return []HardcodedSecretFindingRow{{
		RepoID:       "repo-wire",
		RelativePath: "src/wire/config.go",
		Language:     "go",
		LineNumber:   12,
		LineText:     "apiKey = \"[REDACTED]\"",
		FindingKind:  "api_key",
		Confidence:   "high",
		Severity:     "high",
	}}
}

func (s *codeHardcodedSecretAdmissionCannedStore) InvestigateHardcodedSecrets(_ context.Context, req HardcodedSecretInvestigationRequest) ([]HardcodedSecretFindingRow, error) {
	if req.RepoID == "nothing-matches" {
		return nil, nil
	}
	return s.findings, nil
}

func (s *codeHardcodedSecretAdmissionCannedStore) InvestigateHardcodedSecretsWithSource(_ context.Context, req HardcodedSecretInvestigationRequest) ([]HardcodedSecretFindingRow, HardcodedSecretReadSource, error) {
	if req.RepoID == "nothing-matches" {
		return nil, "", nil
	}
	return s.findings, s.source, nil
}

var _ CodeHardcodedSecretReadAdmission = (*codeHardcodedSecretAdmissionCannedStore)(nil)

// marshalCodeHardcodedSecretAdmissionWire projects one value through the
// same encoding the handlers use (no HTML escaping), so the proof
// compares wire bytes the way a satellite consumer would read them, not
// Go structs that could drift from the wire.
func marshalCodeHardcodedSecretAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestCodeHardcodedSecretReadAdmissionWireShapesAreSatelliteConsumable
// proves a satellite can consume the pin using only the pin plus the
// codequery shapes: both pinned reads answer through a
// CodeHardcodedSecretReadAdmission-typed value, the source-reporting
// twin names its storage path (the label the response shaping depends
// on), the finding rows carry the keys a satellite record would depend
// on, and an unmatching read answers with zero rows and no error.
func TestCodeHardcodedSecretReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin CodeHardcodedSecretReadAdmission = &codeHardcodedSecretAdmissionCannedStore{
		findings: codeHardcodedSecretAdmissionCannedFindings(),
		source:   HardcodedSecretReadSideTable,
	}

	req := HardcodedSecretInvestigationRequest{
		RepoID:               "",
		AllowedRepositoryIDs: []string{"repo-wire"},
	}
	rows, err := pin.InvestigateHardcodedSecrets(ctx, req)
	if err != nil || len(rows) != 1 || rows[0].FindingKind != "api_key" {
		t.Fatalf("InvestigateHardcodedSecrets() = %#v, %v, want the canned finding", rows, err)
	}
	if wire := marshalCodeHardcodedSecretAdmissionWire(t, rows[0]); !strings.Contains(wire, `"RelativePath":"src/wire/config.go"`) || !strings.Contains(wire, `"FindingKind":"api_key"`) || !strings.Contains(wire, `"LineNumber":12`) {
		t.Errorf("finding wire bytes = %s, want relative_path, finding_kind, and line_number keys", wire)
	}

	sourced, source, err := pin.InvestigateHardcodedSecretsWithSource(ctx, req)
	if err != nil || len(sourced) != 1 || source != HardcodedSecretReadSideTable {
		t.Fatalf("InvestigateHardcodedSecretsWithSource() = %#v, %v, %v, want the canned finding labeled side_table", sourced, source, err)
	}
	if wire := marshalCodeHardcodedSecretAdmissionWire(t, source); wire != `"side_table"` {
		t.Errorf("source wire bytes = %s, want the side_table label", wire)
	}

	empty, _, err := pin.InvestigateHardcodedSecretsWithSource(ctx, HardcodedSecretInvestigationRequest{RepoID: "nothing-matches"})
	if err != nil || len(empty) != 0 {
		t.Fatalf("InvestigateHardcodedSecretsWithSource() unmatching = %#v, %v, want zero rows and no error", empty, err)
	}
}
