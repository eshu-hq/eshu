// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"testing"

	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// fullGCPIAMPolicyObservationPayload returns a valid gcp_iam_policy_observation
// payload with every required key present (including the fingerprinted members
// slice), so a test can delete exactly one key and prove decode dead-letters on
// that field.
func fullGCPIAMPolicyObservationPayload() map[string]any {
	return map[string]any{
		"full_resource_name": "//cloudresourcemanager.googleapis.com/projects/demo-proj",
		"asset_type":         "cloudresourcemanager.googleapis.com/Project",
		"role":               "roles/storage.admin",
		"members": []any{
			map[string]any{"member_class": "user", "member_fingerprint": "fp:member:abc123"},
		},
	}
}

// TestDecodeGCPIAMPolicyObservation_MissingMembersDeadLetters is the shaped
// regression for the codex review finding: members is an UNCONDITIONAL emitter
// invariant (gcpcloud.NewIAMPolicyObservationEnvelope rejects an observation
// with zero fingerprinted members before the envelope is built), so it must be
// a required field. It proves a payload carrying every other required key but
// omitting the members key dead-letters as a classified input_invalid naming
// "members", rather than decoding to a zero-value struct with no principal
// evidence. Before members was made required (dropped omitempty) this decoded
// successfully, letting an external collector emit an IAM policy observation
// with no principal evidence and still pass decode + schema conformance.
func TestDecodeGCPIAMPolicyObservation_MissingMembersDeadLetters(t *testing.T) {
	t.Parallel()

	payload := fullGCPIAMPolicyObservationPayload()
	delete(payload, "members") // absent, not merely empty

	env := Envelope{FactKind: FactKindGCPIAMPolicyObservation, SchemaVersion: "1.0.0", Payload: payload}
	got, err := DecodeGCPIAMPolicyObservation(env)
	if err == nil {
		t.Fatalf("DecodeGCPIAMPolicyObservation() error = nil, want non-nil for missing required members")
	}

	var classified *DecodeError
	if !errors.As(err, &classified) {
		t.Fatalf("DecodeGCPIAMPolicyObservation() error = %T, want *DecodeError", err)
	}
	if classified.Classification != ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
	}
	if classified.Field != "members" {
		t.Fatalf("Field = %q, want %q", classified.Field, "members")
	}

	var zero gcpv1.IAMPolicyObservation
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodeGCPIAMPolicyObservation() returned non-zero struct %+v on error, want zero value", got)
	}
}

// TestDecodeGCPIAMPolicyObservation_FullPayloadDecodes is the positive
// counterpart: a payload carrying every required key (members included) decodes
// cleanly, so the missing-members assertion above cannot pass merely because
// decode always errors.
func TestDecodeGCPIAMPolicyObservation_FullPayloadDecodes(t *testing.T) {
	t.Parallel()

	env := Envelope{FactKind: FactKindGCPIAMPolicyObservation, SchemaVersion: "1.0.0", Payload: fullGCPIAMPolicyObservationPayload()}
	got, err := DecodeGCPIAMPolicyObservation(env)
	if err != nil {
		t.Fatalf("DecodeGCPIAMPolicyObservation() error = %v, want nil for a full required payload", err)
	}
	if len(got.Members) != 1 {
		t.Fatalf("Members = %v, want one decoded member binding", got.Members)
	}
	if got.Members[0]["member_fingerprint"] != "fp:member:abc123" {
		t.Fatalf("Members[0] = %v, want the fingerprinted member preserved", got.Members[0])
	}
}

// requiredCollectionKey identifies one intentionally-required slice/map field
// by its fact kind and json key name.
type requiredCollectionKey struct {
	factKind string
	jsonName string
}

// intentionalRequiredCollections is the explicit allow-list of slice/map fields
// that are REQUIRED (no omitempty) on purpose. A required collection is correct
// only when the emitter unconditionally writes the key, so each entry documents
// which emitter invariant justifies it. Everything else must stay optional
// (omitempty) so a nil/absent collection never dead-letters a valid fact.
// TestPayloadStructShapeConvention (decode_test.go) reads this allow-list.
var intentionalRequiredCollections = map[requiredCollectionKey]struct{}{
	// gcp_iam_policy_observation.members: gcpcloud.NewIAMPolicyObservationEnvelope
	// (iam_policy_observation.go:84-86) rejects an observation with zero
	// fingerprinted members before the envelope is built, so members is the
	// binding's unconditional principal evidence — an absent members key must
	// dead-letter, not decode to a struct with no principal.
	{FactKindGCPIAMPolicyObservation, "members"}: {},
	// gcp_tag_observation.tag_value_fingerprints: gcpcloud.NewTagObservationEnvelope
	// rejects an observation with zero fingerprinted tag values before the
	// envelope is built, so this map is the tag fact's unconditional evidence.
	{FactKindGCPTagObservation, "tag_value_fingerprints"}: {},
	// azure_tag_observation.tag_value_fingerprints:
	// azurecloud.NewTagObservationEnvelope rejects an observation with zero
	// fingerprinted tag values before the envelope is built.
	{FactKindAzureTagObservation, "tag_value_fingerprints"}: {},
	// file.parsed_file_data: fileFactEnvelope
	// (go/internal/collector/git_fact_builder.go:433-439) unconditionally sets
	// parsed_file_data to the parser's per-file map on every "file" fact it
	// builds — there is no conditional-emission path that could omit it for a
	// valid fact. An absent parsed_file_data must dead-letter, not decode to a
	// File with a nil parser payload the code-graph-core reducer handlers
	// would then silently skip.
	{FactKindCodegraphFile, "parsed_file_data"}: {},
	// codeowners.ownership.owners: a CODEOWNERS pattern line with zero owner
	// tokens carries no ownership claim at all, so the collector never emits
	// an Ownership fact for one (codeowners/v1/ownership.go doc comment).
	// Issue #5419 Phase 1.
	{FactKindCodeownersOwnership, "owners"}: {},
	// reducer_supply_chain_impact_finding collection keys are emitted
	// unconditionally by PostgresSupplyChainImpactWriter. Empty collections are
	// encoded as [] rather than null, so an absent key indicates a malformed
	// governed reducer-derived payload.
	{FactKindReducerSupplyChainImpactFinding, "priority_reason_codes"}:  {},
	{FactKindReducerSupplyChainImpactFinding, "priority_contributions"}: {},
	{FactKindReducerSupplyChainImpactFinding, "workload_ids"}:           {},
	{FactKindReducerSupplyChainImpactFinding, "deployment_ids"}:         {},
	{FactKindReducerSupplyChainImpactFinding, "service_ids"}:            {},
	{FactKindReducerSupplyChainImpactFinding, "environments"}:           {},
	{FactKindReducerSupplyChainImpactFinding, "catalog_entity_refs"}:    {},
	{FactKindReducerSupplyChainImpactFinding, "catalog_owner_refs"}:     {},
	{FactKindReducerSupplyChainImpactFinding, "missing_evidence"}:       {},
	{FactKindReducerSupplyChainImpactFinding, "evidence_path"}:          {},
	{FactKindReducerSupplyChainImpactFinding, "evidence_fact_ids"}:      {},
	{FactKindReducerSupplyChainImpactFinding, "source_layers"}:          {},
	// Runtime drift writers emit these collections unconditionally and
	// normalize empty values to [] so the read model never sees a null list.
	{FactKindReducerAWSCloudRuntimeDriftFinding, "missing_evidence"}:   {},
	{FactKindReducerAWSCloudRuntimeDriftFinding, "warning_flags"}:      {},
	{FactKindReducerAWSCloudRuntimeDriftFinding, "evidence"}:           {},
	{FactKindReducerAWSCloudRuntimeDriftFinding, "source_layers"}:      {},
	{FactKindReducerMultiCloudRuntimeDriftFinding, "missing_evidence"}: {},
	{FactKindReducerMultiCloudRuntimeDriftFinding, "warning_flags"}:    {},
	{FactKindReducerMultiCloudRuntimeDriftFinding, "evidence"}:         {},
	{FactKindReducerMultiCloudRuntimeDriftFinding, "source_layers"}:    {},
}
