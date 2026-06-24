// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestEC2InstanceNodeMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: &recordingEC2InstanceNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestEC2InstanceNodeMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := EC2InstanceNodeMaterializationHandler{
		NodeWriter: &recordingEC2InstanceNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceNodeMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestEC2InstanceNodeMaterializationRequiresNodeWriter(t *testing.T) {
	t.Parallel()

	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceNodeMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

func TestExtractEC2InstanceNodeRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	if rows := ExtractEC2InstanceNodeRows(nil); rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
}

// TestExtractEC2InstanceNodeRowsBuildsCanonicalUID proves the node uses the exact
// cloudResourceUID(account, region, "aws_ec2_instance", instance_id) scheme that
// observability_coverage_correlation_test.go already assumes, so an alarm whose
// InstanceId dimension resolves to this instance resolves to a node that exists.
func TestExtractEC2InstanceNodeRowsBuildsCanonicalUID(t *testing.T) {
	t.Parallel()

	const instanceID = "i-0abc123"
	envelopes := []facts.Envelope{
		ec2InstancePostureEnvelope(sampleEC2PosturePayload(instanceID)),
	}

	rows := ExtractEC2InstanceNodeRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	want := cloudResourceUID("111122223333", "us-east-1", "aws_ec2_instance", instanceID)
	if got := anyToString(rows[0]["uid"]); got != want {
		t.Fatalf("uid = %q, want %q (the canonical EC2 instance CloudResource uid)", got, want)
	}
	if got := anyToString(rows[0]["resource_type"]); got != "aws_ec2_instance" {
		t.Fatalf("resource_type = %q, want aws_ec2_instance", got)
	}
	if got := anyToString(rows[0]["resource_id"]); got != instanceID {
		t.Fatalf("resource_id = %q, want %q", got, instanceID)
	}
	if got := anyToString(rows[0]["name"]); got != instanceID {
		t.Fatalf("name = %q, want the instance id (no tag value is read)", got)
	}
	if got := anyToString(rows[0]["instance_profile_arn"]); got != "arn:aws:iam::111122223333:instance-profile/app" {
		t.Fatalf("instance_profile_arn = %q, want the profile arn property", got)
	}
}

// TestExtractEC2InstanceNodeRowsCarriesSafePostureOnly proves the node carries the
// derived posture booleans/scalars but NEVER the raw public IP, user-data content,
// block devices, or fabricated topology fields the posture fact does not carry.
func TestExtractEC2InstanceNodeRowsCarriesSafePostureOnly(t *testing.T) {
	t.Parallel()

	rows := ExtractEC2InstanceNodeRows([]facts.Envelope{
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-0abc123")),
	})
	row := rows[0]

	// Present, safe posture fields.
	if row["imds_v2_required"] != true {
		t.Fatalf("imds_v2_required = %v, want true", row["imds_v2_required"])
	}
	if row["public_ip_associated"] != true {
		t.Fatalf("public_ip_associated = %v, want true", row["public_ip_associated"])
	}
	if got := anyToString(row["tenancy"]); got != "default" {
		t.Fatalf("tenancy = %q, want default", got)
	}

	// Excluded surfaces: the raw public IP must never reach the node.
	if _, present := row["public_ip_address"]; present {
		t.Fatal("row must not carry public_ip_address (raw routable identifier)")
	}
	// Topology fields the posture fact never carries must not be fabricated.
	for _, absent := range []string{"instance_type", "availability_zone", "vpc_id", "subnet_id", "block_devices"} {
		if _, present := row[absent]; present {
			t.Fatalf("row must not fabricate %q (not on the posture fact)", absent)
		}
	}
}

// TestExtractEC2InstanceNodeRowsMissingOptionalFields proves a sparse posture fact
// still materializes a node with the present fields and never fabricates absent
// optional posture data.
func TestExtractEC2InstanceNodeRowsMissingOptionalFields(t *testing.T) {
	t.Parallel()

	rows := ExtractEC2InstanceNodeRows([]facts.Envelope{
		ec2InstancePostureEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_instance",
			"instance_id":   "i-sparse",
			"state":         "running",
		}),
	})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (sparse fact still materializes)", len(rows))
	}
	row := rows[0]
	// Absent optional booleans/scalars stay nil — never fabricated to false/zero.
	if row["imds_v2_required"] != nil {
		t.Fatalf("imds_v2_required = %v, want nil for an unreported field", row["imds_v2_required"])
	}
	if row["imds_http_put_hop_limit"] != nil {
		t.Fatalf("imds_http_put_hop_limit = %v, want nil for an unreported field", row["imds_http_put_hop_limit"])
	}
	if got := anyToString(row["instance_profile_arn"]); got != "" {
		t.Fatalf("instance_profile_arn = %q, want empty for an instance with no profile", got)
	}
}

func TestExtractEC2InstanceNodeRowsFallsBackToARNIdentity(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:ec2:us-east-1:111122223333:instance/i-fromarn"
	rows := ExtractEC2InstanceNodeRows([]facts.Envelope{
		ec2InstancePostureEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_instance",
			"arn":           arn,
			// instance_id intentionally blank -> identity falls back to the ARN,
			// mirroring the posture envelope's own identity derivation.
		}),
	})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	want := cloudResourceUID("111122223333", "us-east-1", "aws_ec2_instance", arn)
	if got := anyToString(rows[0]["uid"]); got != want {
		t.Fatalf("uid = %q, want %q (ARN-fallback identity)", got, want)
	}
}

func TestExtractEC2InstanceNodeRowsRequiresIdentity(t *testing.T) {
	t.Parallel()

	rows := ExtractEC2InstanceNodeRows([]facts.Envelope{
		ec2InstancePostureEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_instance",
			// no instance_id, no arn -> cannot form a uid.
		}),
	})
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (no identity must not fabricate a node)", len(rows))
	}
}

func TestExtractEC2InstanceNodeRowsSkipsNonPostureFacts(t *testing.T) {
	t.Parallel()

	rows := ExtractEC2InstanceNodeRows([]facts.Envelope{
		{FactKind: facts.AWSResourceFactKind, Payload: map[string]any{"resource_id": "ignored"}},
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-0abc123")),
	})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (non-posture facts must be skipped)", len(rows))
	}
}

func TestExtractEC2InstanceNodeRowsSkipsTombstone(t *testing.T) {
	t.Parallel()

	tombstone := ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-terminated"))
	tombstone.IsTombstone = true
	rows := ExtractEC2InstanceNodeRows([]facts.Envelope{
		tombstone,
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-0abc123")),
	})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (a terminated instance must not materialize)", len(rows))
	}
	want := cloudResourceUID("111122223333", "us-east-1", "aws_ec2_instance", "i-0abc123")
	if got := anyToString(rows[0]["uid"]); got != want {
		t.Fatalf("uid = %q, want the live instance uid", got)
	}
}

func TestExtractEC2InstanceNodeRowsDeduplicatesByUID(t *testing.T) {
	t.Parallel()

	payload := sampleEC2PosturePayload("i-0abc123")
	rows := ExtractEC2InstanceNodeRows([]facts.Envelope{
		ec2InstancePostureEnvelope(payload),
		ec2InstancePostureEnvelope(payload),
	})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (duplicate facts converge on one node)", len(rows))
	}
}

func TestExtractEC2InstanceNodeRowsDeterministicOrderRegardlessOfInput(t *testing.T) {
	t.Parallel()

	forward := ExtractEC2InstanceNodeRows([]facts.Envelope{
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-aaa")),
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-bbb")),
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-ccc")),
	})
	reverse := ExtractEC2InstanceNodeRows([]facts.Envelope{
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-ccc")),
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-bbb")),
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-aaa")),
	})
	if len(forward) != 3 || len(reverse) != 3 {
		t.Fatalf("len(forward)=%d len(reverse)=%d, want 3 each", len(forward), len(reverse))
	}
	for i := range forward {
		if anyToString(forward[i]["uid"]) != anyToString(reverse[i]["uid"]) {
			t.Fatalf("row %d uid differs by input order: %q vs %q (uid must be deterministic)",
				i, forward[i]["uid"], reverse[i]["uid"])
		}
	}
}

func TestEC2InstanceNodeMaterializationHandleWritesNodes(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2InstanceNodeWriter{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-aaa")),
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-bbb")),
	}}
	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader: loader,
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceNodeMaterialization,
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
	if writer.evidenceSource != ec2InstanceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", writer.evidenceSource, ec2InstanceEvidenceSource)
	}
}

func TestEC2InstanceNodeMaterializationHandleNoFactsIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2InstanceNodeWriter{}
	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceNodeMaterialization,
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

// TestEC2InstanceNodeMaterializationPublishesCloudResourcePhase proves the node
// domain publishes the cloud_resource_uid / canonical_nodes_committed phase under
// the distinct ec2_instance_node_materialization entity key, so the future
// USES_PROFILE edge (PR-B) gates on instance-node readiness independently of the
// aws_resource node phase.
func TestEC2InstanceNodeMaterializationPublishesCloudResourcePhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-aaa")),
	}}
	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     &recordingEC2InstanceNodeWriter{},
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceNodeMaterialization,
		EntityKeys:   []string{"ec2_instance_node_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (PR-B gates on this readiness phase)", len(publisher.calls))
	}
	row := publisher.calls[0][0]
	if got, want := row.Key.Keyspace, GraphProjectionKeyspaceCloudResourceUID; got != want {
		t.Fatalf("keyspace = %q, want %q (EC2 instances are CloudResource nodes)", got, want)
	}
	if got, want := row.Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("phase = %q, want %q", got, want)
	}
	if got, want := row.Key.AcceptanceUnitID, "ec2_instance_node_materialization:scope-1"; got != want {
		t.Fatalf("acceptance unit = %q, want the distinct EC2 node entity key %q", got, want)
	}
}

func TestEC2InstanceNodeMaterializationPublishesPhaseOnEmptyGeneration(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingEC2InstanceNodeWriter{}
	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader:     &stubFactLoader{},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceNodeMaterialization,
		EntityKeys:   []string{"ec2_instance_node_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (no facts must not write)", writer.calls)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (empty generation must still unblock PR-B)", len(publisher.calls))
	}
}

func TestEC2InstanceNodeMaterializationDoesNotPublishPhaseOnWriteFailure(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingEC2InstanceNodeWriter{err: errors.New("graph backend unavailable")}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-aaa")),
	}}
	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceNodeMaterialization,
		EntityKeys:   []string{"ec2_instance_node_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err == nil {
		t.Fatal("expected error when node write fails")
	}

	if len(publisher.calls) != 0 {
		t.Fatalf("publisher.calls = %d, want 0 (no readiness gate after a failed write)", len(publisher.calls))
	}
}
