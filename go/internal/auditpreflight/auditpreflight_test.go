// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditpreflight

import "testing"

const validIssue = `### Competitor source and local path

graphify at /local/graphify

### Eshu code evidence

go/internal/query/capabilities.go serves the catalog

### Eshu docs evidence

docs/public/reference/capability-catalog.md

### Eshu test or proof evidence

go test ./internal/query

### Existing issue duplicate search

searched "capability catalog" — found #2711 epic, no duplicate

### Gap class

foundation exists

### Owner surface

api

### Verification plan

go run ./cmd/capability-inventory -mode verify
`

func TestValidatePassesCompleteIssue(t *testing.T) {
	t.Parallel()
	findings := Validate(validIssue)
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestValidateFlagsMissingFields(t *testing.T) {
	t.Parallel()
	body := `### Gap class

missing

### Owner surface

api
`
	findings := Validate(body)
	kinds := map[FindingKind]int{}
	for _, f := range findings {
		kinds[f.Kind]++
	}
	// Six required sections are absent (all but gap class and owner surface).
	if kinds[FindingMissingField] != 6 {
		t.Fatalf("missing field findings = %d, want 6: %+v", kinds[FindingMissingField], findings)
	}
}

func TestValidateFlagsEmptyAndNoResponse(t *testing.T) {
	t.Parallel()
	body := replaceSection(validIssue, "Eshu code evidence", "_No response_")
	findings := Validate(body)
	found := false
	for _, f := range findings {
		if f.Kind == FindingEmptyField && f.Field == "Eshu code evidence" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected empty-field finding for code evidence: %+v", findings)
	}
}

func TestValidateFlagsInvalidGapClassAndOwner(t *testing.T) {
	t.Parallel()
	body := replaceSection(validIssue, "Gap class", "totally bogus")
	body = replaceSection(body, "Owner surface", "teapot")
	findings := Validate(body)
	kinds := map[FindingKind]int{}
	for _, f := range findings {
		kinds[f.Kind]++
	}
	if kinds[FindingInvalidGapClass] != 1 {
		t.Fatalf("invalid gap class findings = %d: %+v", kinds[FindingInvalidGapClass], findings)
	}
	if kinds[FindingInvalidOwnerSurface] != 1 {
		t.Fatalf("invalid owner surface findings = %d: %+v", kinds[FindingInvalidOwnerSurface], findings)
	}
}

func TestValidateNormalizesCaseAndSpacing(t *testing.T) {
	t.Parallel()
	body := replaceSection(validIssue, "Gap class", "  Foundation Exists  ")
	body = replaceSection(body, "Owner surface", "API")
	if findings := Validate(body); len(findings) != 0 {
		t.Fatalf("normalized values should pass: %+v", findings)
	}
}
