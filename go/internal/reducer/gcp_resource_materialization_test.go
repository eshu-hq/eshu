// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// gcpResourceEnvelope builds a gcp_cloud_resource fact envelope for tests from
// the bounded set of payload fields the node projection reads.
func gcpResourceEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.GCPCloudResourceFactKind,
		Payload:  payload,
	}
}

func TestGCPResourceMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := GCPResourceMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: &recordingCloudResourceNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestGCPResourceMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := GCPResourceMaterializationHandler{
		NodeWriter: &recordingCloudResourceNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestGCPResourceMaterializationRequiresNodeWriter(t *testing.T) {
	t.Parallel()

	handler := GCPResourceMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

func TestExtractGCPCloudResourceNodeRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	rows, quarantined, err := ExtractGCPCloudResourceNodeRows(nil)
	if err != nil {
		t.Fatalf("ExtractGCPCloudResourceNodeRows() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
	if quarantined != nil {
		t.Fatalf("quarantined = %v, want nil", quarantined)
	}
}

func TestExtractGCPCloudResourceNodeRowsBuildsStableUID(t *testing.T) {
	t.Parallel()

	const fullName = "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/app"
	envelopes := []facts.Envelope{
		gcpResourceEnvelope(map[string]any{
			"full_resource_name": fullName,
			"asset_type":         "compute.googleapis.com/Instance",
			"project_id":         "demo-proj",
			"location":           "us-central1-a",
			"asset_type_family":  "compute",
			"display_name":       "app",
			"state":              "RUNNING",
		}),
	}

	rows, _, err := ExtractGCPCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractGCPCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}

	wantUID := cloudResourceUID("demo-proj", "us-central1-a", "compute.googleapis.com/Instance", fullName)
	if got := anyToString(rows[0]["uid"]); got != wantUID {
		t.Fatalf("uid = %q, want %q", got, wantUID)
	}
	if got := anyToString(rows[0]["resource_id"]); got != fullName {
		t.Fatalf("resource_id = %q, want %q (GCP joins resolve on the full resource name)", got, fullName)
	}
	if got := anyToString(rows[0]["resource_type"]); got != "compute.googleapis.com/Instance" {
		t.Fatalf("resource_type = %q", got)
	}
	if got := anyToString(rows[0]["account_id"]); got != "demo-proj" {
		t.Fatalf("account_id = %q, want project id demo-proj", got)
	}
	if got := anyToString(rows[0]["region"]); got != "us-central1-a" {
		t.Fatalf("region = %q, want location us-central1-a", got)
	}
	if got := anyToString(rows[0]["name"]); got != "app" {
		t.Fatalf("name = %q, want display_name app", got)
	}
	if got := anyToString(rows[0]["service_kind"]); got != "compute" {
		t.Fatalf("service_kind = %q, want asset_type_family compute", got)
	}
	if got := anyToString(rows[0]["arn"]); got != "" {
		t.Fatalf("arn = %q, want empty for GCP", got)
	}
}

// TestExtractGCPCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeys proves
// the regression for issue #4995: every GCP CloudResource row must carry an
// explicit (present-key) empty value for the 7 anchor/identity keys
// (workload_id, service_name, and the 5 service_anchor_* keys) the shared
// canonicalCloudResourceUpsertCypher SET clause always reads (see
// go/internal/reducer/README.md's "#4995" entry for the full NornicDB proof:
// a missing UNWIND row map key does not evaluate to null in a SET clause on
// the pinned backend, it stringifies the row expression instead). Checking
// key presence with `_, ok := row[key]` (not just value equality) is
// required: a missing key and an explicit "" both stringify to "" via
// anyToString, so a value-only assertion would pass either way.
func TestExtractGCPCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeys(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		gcpResourceEnvelope(map[string]any{
			"full_resource_name": "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/app",
			"asset_type":         "compute.googleapis.com/Instance",
			"project_id":         "demo-proj",
			"location":           "us-central1-a",
		}),
	}

	rows, _, err := ExtractGCPCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractGCPCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]

	for _, key := range []string{
		"workload_id",
		"service_name",
		"service_anchor_status",
		"service_anchor_source",
		"service_anchor_reason",
		"running_image_ref",
		"running_image_digest",
	} {
		value, ok := row[key]
		if !ok {
			t.Fatalf("row[%q] is absent; the shared upsert Cypher's row.%s reference "+
				"would resolve against a missing map key on the pinned NornicDB backend "+
				"and persist a stringified-row literal instead of an empty value", key, key)
		}
		if value != "" {
			t.Fatalf("row[%q] = %#v, want empty string (GCP has no service-anchor source today)", key, value)
		}
	}

	namesValue, ok := row["service_anchor_names"]
	if !ok {
		t.Fatal("row[\"service_anchor_names\"] is absent; must be an explicit empty value")
	}
	names, ok := namesValue.([]string)
	if !ok {
		t.Fatalf("row[\"service_anchor_names\"] type = %T, want []string", namesValue)
	}
	if len(names) != 0 {
		t.Fatalf("row[\"service_anchor_names\"] = %#v, want empty slice", names)
	}

	tokensValue, ok := row["service_anchor_name_tokens"]
	if !ok {
		t.Fatal("row[\"service_anchor_name_tokens\"] is absent; must be an explicit empty value")
	}
	if tokensValue != "" {
		t.Fatalf("row[\"service_anchor_name_tokens\"] = %#v, want empty string", tokensValue)
	}
}

func TestExtractGCPCloudResourceNodeRowsSkipsNonResourceFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: facts.GCPCloudRelationshipFactKind, Payload: map[string]any{"source_full_resource_name": "ignored"}},
		gcpResourceEnvelope(map[string]any{
			"full_resource_name": "//storage.googleapis.com/projects/_/buckets/b",
			"asset_type":         "storage.googleapis.com/Bucket",
			"project_id":         "demo-proj",
		}),
	}

	rows, _, err := ExtractGCPCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractGCPCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (relationship facts must be skipped)", len(rows))
	}
	if got := anyToString(rows[0]["resource_id"]); got != "//storage.googleapis.com/projects/_/buckets/b" {
		t.Fatalf("resource_id = %q", got)
	}
}

// TestExtractGCPCloudResourceNodeRowsRequiresIdentity proves an ABSENT required
// identity field (full_resource_name or asset_type) is quarantined as an
// input_invalid dead-letter rather than silently producing zero rows with no
// operator signal — the accuracy guarantee the typed-decode migration adds.
func TestExtractGCPCloudResourceNodeRowsRequiresIdentity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		// Missing full_resource_name (absent, not empty).
		gcpResourceEnvelope(map[string]any{
			"asset_type": "compute.googleapis.com/Instance",
			"project_id": "demo-proj",
		}),
		// Missing asset_type (absent, not empty).
		gcpResourceEnvelope(map[string]any{
			"full_resource_name": "//compute.googleapis.com/projects/demo-proj/zones/z/instances/i",
			"project_id":         "demo-proj",
		}),
	}

	rows, quarantined, err := ExtractGCPCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractGCPCloudResourceNodeRows() error = %v, want nil (per-fact quarantine, not a fatal error)", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for two facts each missing a required identity field", len(rows))
	}
	if len(quarantined) != 2 {
		t.Fatalf("len(quarantined) = %d, want 2 (both facts missing a required identity field must be quarantined)", len(quarantined))
	}
}

// TestExtractGCPCloudResourceNodeRowsPresentButEmptyIdentityIsDropped proves a
// PRESENT-but-empty identity field is a valid decode (not a quarantine) that is
// still dropped as an incomplete, non-materializable node — mirroring the
// AWS-side present-but-empty distinction.
func TestExtractGCPCloudResourceNodeRowsPresentButEmptyIdentityIsDropped(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		gcpResourceEnvelope(map[string]any{
			"full_resource_name": "",
			"asset_type":         "compute.googleapis.com/Instance",
			"project_id":         "demo-proj",
		}),
	}

	rows, quarantined, err := ExtractGCPCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractGCPCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for a present-but-empty full_resource_name", len(rows))
	}
	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a present-but-empty required field is a valid decode, not a quarantine", len(quarantined))
	}
}

func TestExtractGCPCloudResourceNodeRowsDeduplicatesByUID(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"full_resource_name": "//compute.googleapis.com/projects/demo-proj/zones/z/instances/i",
		"asset_type":         "compute.googleapis.com/Instance",
		"project_id":         "demo-proj",
		"location":           "us-central1-a",
		"display_name":       "i",
	}
	envelopes := []facts.Envelope{
		gcpResourceEnvelope(payload),
		gcpResourceEnvelope(payload),
	}

	rows, _, err := ExtractGCPCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractGCPCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (duplicate facts must converge on one node)", len(rows))
	}
}

func TestGCPResourceMaterializationHandleWritesNodes(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceNodeWriter{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		gcpResourceEnvelope(map[string]any{
			"full_resource_name": "//compute.googleapis.com/projects/demo-proj/zones/z/instances/i",
			"asset_type":         "compute.googleapis.com/Instance",
			"project_id":         "demo-proj",
		}),
		gcpResourceEnvelope(map[string]any{
			"full_resource_name": "//storage.googleapis.com/projects/_/buckets/b",
			"asset_type":         "storage.googleapis.com/Bucket",
			"project_id":         "demo-proj",
		}),
	}}

	handler := GCPResourceMaterializationHandler{
		FactLoader: loader,
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	if len(writer.rows) != 2 {
		t.Fatalf("len(writer.rows) = %d, want 2", len(writer.rows))
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}
	if writer.evidenceSource != gcpResourceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", writer.evidenceSource, gcpResourceEvidenceSource)
	}
}

func TestGCPResourceMaterializationHandleNoFactsIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceNodeWriter{}
	handler := GCPResourceMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (no facts must not write)", writer.calls)
	}
}

func TestGCPResourceMaterializationHandlePublishesCanonicalNodesCommittedPhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		gcpResourceEnvelope(map[string]any{
			"full_resource_name": "//compute.googleapis.com/projects/demo-proj/zones/z/instances/i",
			"asset_type":         "compute.googleapis.com/Instance",
			"project_id":         "demo-proj",
		}),
	}}
	handler := GCPResourceMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     &recordingCloudResourceNodeWriter{},
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPResourceMaterialization,
		// EntityKeys carries the GCP acceptance unit so the published phase
		// keys to a distinct acceptance unit from the AWS materialization.
		EntityKeys:  []string{"gcp_resource_materialization:scope-1"},
		EnqueuedAt:  time.Now(),
		AvailableAt: time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (Stage B gates on this readiness phase)", len(publisher.calls))
	}
	rows := publisher.calls[0]
	if len(rows) != 1 {
		t.Fatalf("published rows = %d, want 1", len(rows))
	}
	if got, want := rows[0].Key.Keyspace, GraphProjectionKeyspaceCloudResourceUID; got != want {
		t.Fatalf("keyspace = %q, want %q", got, want)
	}
	if got, want := rows[0].Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("phase = %q, want %q", got, want)
	}
	if got, want := rows[0].Key.AcceptanceUnitID, "gcp_resource_materialization:scope-1"; got != want {
		t.Fatalf("acceptance unit = %q, want %q (must not collide with the AWS unit)", got, want)
	}
	if got, want := rows[0].Key.ScopeID, "scope-1"; got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}
}

func TestGCPResourceMaterializationHandlePublishesPhaseOnEmptyGeneration(t *testing.T) {
	t.Parallel()

	// A generation that scanned zero materializable GCP resources must still
	// publish the canonical-nodes-committed phase, otherwise the GCP relationship
	// edge projection (#2348) never observes that this stage completed and blocks
	// forever on the readiness gate.
	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingCloudResourceNodeWriter{}
	handler := GCPResourceMaterializationHandler{
		FactLoader:     &stubFactLoader{},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPResourceMaterialization,
		EntityKeys:   []string{"gcp_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (no facts must not write)", writer.calls)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (empty generation must still unblock the edge projection)", len(publisher.calls))
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("phase = %q, want %q", got, want)
	}
}
