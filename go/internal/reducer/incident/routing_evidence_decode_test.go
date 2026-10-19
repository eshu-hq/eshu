// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"reflect"
	"testing"
)

// TestBuildIncidentRoutingEvidenceInputsMatchesBuilder pins the exported
// cross-package seam to the unexported builder: same raw evidence in, byte-
// identical packets and quarantine out. The wrapper exists so offline guards
// drive the production builder; any behavioral drift between the two would
// let a guard prove something the handler does not run.
func TestBuildIncidentRoutingEvidenceInputsMatchesBuilder(t *testing.T) {
	t.Parallel()

	raw := exactIncidentRoutingRawEvidence(t)

	wantInputs, wantQuarantined, err := buildIncidentRoutingEvidenceInputs(raw)
	if err != nil {
		t.Fatalf("buildIncidentRoutingEvidenceInputs() error = %v", err)
	}
	gotInputs, gotQuarantined, err := BuildIncidentRoutingEvidenceInputs(raw)
	if err != nil {
		t.Fatalf("BuildIncidentRoutingEvidenceInputs() error = %v", err)
	}
	if !reflect.DeepEqual(gotInputs, wantInputs) {
		t.Fatalf("BuildIncidentRoutingEvidenceInputs() inputs differ from buildIncidentRoutingEvidenceInputs():\n got %#v\nwant %#v", gotInputs, wantInputs)
	}
	if !reflect.DeepEqual(gotQuarantined, wantQuarantined) {
		t.Fatalf("BuildIncidentRoutingEvidenceInputs() quarantined differ:\n got %#v\nwant %#v", gotQuarantined, wantQuarantined)
	}
}

// TestDecodeIncidentRoutingDeclaredEvidenceMapsMetadataFields pins the single
// metadata-to-Declared field mapping the Postgres loader calls per scanned
// content_entities row: every field reads its metadata key, surrounding
// whitespace is trimmed, and absent keys read as their empty value — the
// pre-typing loader's raw map-read behavior, not a quarantine.
func TestDecodeIncidentRoutingDeclaredEvidenceMapsMetadataFields(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{
		"declaration_kind":        "terraform_module",
		"source_class":            "declared",
		"outcome":                 "declared",
		"service_name":            "  Checkout API  ",
		"service_name_resolution": "literal",
		"escalation_policy":       "PEP1",
		"environment":             "prod",
		"workspace":               "prod",
		"redaction_state":         "redacted",
		"unsupported_reason":      "",
		"duplicate_service_name":  false,
		"unrelated_future_key":    "ignored",
	}

	got := DecodeIncidentRoutingDeclaredEvidence(
		"declared-entity-1",
		"repo-observability",
		"pagerduty/main.tf",
		"module.checkout",
		17,
		metadata,
	)

	want := IncidentRoutingDeclaredEvidence{
		EntityID:              "declared-entity-1",
		RepoID:                "repo-observability",
		RelativePath:          "pagerduty/main.tf",
		EntityName:            "module.checkout",
		DeclarationKind:       "terraform_module",
		SourceClass:           "declared",
		Outcome:               "declared",
		ServiceName:           "Checkout API",
		ServiceNameResolution: "literal",
		EscalationPolicy:      "PEP1",
		Environment:           "prod",
		Workspace:             "prod",
		RedactionState:        "redacted",
		DuplicateServiceName:  false,
		StartLine:             17,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DecodeIncidentRoutingDeclaredEvidence() =\n got %#v\nwant %#v", got, want)
	}
}

// TestDecodeIncidentRoutingDeclaredEvidenceToleratesSparseMetadata pins the
// loader-parity half of the decode: a row whose metadata carries only the
// parser-guaranteed keys still decodes, with every other field empty and the
// duplicate flag false — never an error, never a quarantine.
func TestDecodeIncidentRoutingDeclaredEvidenceToleratesSparseMetadata(t *testing.T) {
	t.Parallel()

	got := DecodeIncidentRoutingDeclaredEvidence(
		"declared-entity-9",
		"repo-observability",
		"pagerduty/main.tf",
		"module.checkout",
		3,
		map[string]any{
			"source_class": "declared",
			"service_name": "Checkout API",
		},
	)

	if got.ServiceName != "Checkout API" || got.SourceClass != "declared" {
		t.Fatalf("sparse decode lost carried fields: %#v", got)
	}
	if got.DeclarationKind != "" || got.Outcome != "" || got.ServiceNameResolution != "" ||
		got.EscalationPolicy != "" || got.Environment != "" || got.Workspace != "" ||
		got.RedactionState != "" || got.UnsupportedReason != "" {
		t.Fatalf("sparse decode invented absent fields: %#v", got)
	}
	if got.DuplicateServiceName {
		t.Fatalf("sparse decode defaulted duplicate flag true: %#v", got)
	}
	if got.StartLine != 3 || got.EntityID != "declared-entity-9" {
		t.Fatalf("sparse decode lost scanned columns: %#v", got)
	}
}

// TestDecodeIncidentRoutingDeclaredEvidenceReadsScalarForms pins the exact
// pre-typing coercions for metadata values that arrive in non-string JSON
// forms: numbers render trimmed, and only a true boolean admits the
// duplicate-service-name flag.
func TestDecodeIncidentRoutingDeclaredEvidenceReadsScalarForms(t *testing.T) {
	t.Parallel()

	got := DecodeIncidentRoutingDeclaredEvidence(
		"e", "r", "p", "n", 1,
		map[string]any{
			"service_name":           42,
			"duplicate_service_name": "yes",
		},
	)
	if got.ServiceName != "42" {
		t.Fatalf("numeric service_name = %q, want %q", got.ServiceName, "42")
	}
	if got.DuplicateServiceName {
		t.Fatalf("string duplicate_service_name admitted the flag: %#v", got)
	}

	got = DecodeIncidentRoutingDeclaredEvidence(
		"e", "r", "p", "n", 1,
		map[string]any{"duplicate_service_name": true},
	)
	if !got.DuplicateServiceName {
		t.Fatalf("true duplicate_service_name dropped the flag: %#v", got)
	}
}
