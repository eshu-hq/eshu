// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// This file holds PostgresContainerImageIdentityWriter tests, split out of
// container_image_identity_test.go to keep that file under the package's
// 500-line cap. testContainerDigest/testOtherContainerDigest and the stub
// types they share stay defined in container_image_identity_test.go.

func TestPostgresContainerImageIdentityWriterPersistsCanonicalDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Cause:        "container image references observed",
		Decisions: []ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/api:prod",
				Digest:           testContainerDigest,
				RepositoryID:     "oci-registry://registry.example.com/team/api",
				Outcome:          ContainerImageIdentityTagResolved,
				Reason:           "tag resolved to one registry digest observation",
				CanonicalWrites:  1,
				EvidenceFactIDs:  []string{"git-tag", "oci-tag"},
				IdentityStrength: "tag_observation_with_digest",
			},
			{
				ImageRef:        "registry.example.com/team/missing:prod",
				Outcome:         ContainerImageIdentityUnresolved,
				Reason:          "no registry digest observation matched the image reference",
				CanonicalWrites: 0,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}
	if got, want := rows[0].FactKind, containerImageIdentityFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(rows[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["digest"], testContainerDigest; got != want {
		t.Fatalf("payload digest = %#v, want %q", got, want)
	}
	if got, want := payload["identity_strength"], "tag_observation_with_digest"; got != want {
		t.Fatalf("payload identity_strength = %#v, want %q", got, want)
	}
}

func TestPostgresContainerImageIdentityWriterUsesStableTagReferenceIdentity(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{
		DB: db,
	}
	write := ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Cause:        "container image references observed",
		Decisions: []ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/api:prod",
				Digest:           testContainerDigest,
				RepositoryID:     "oci-registry://registry.example.com/team/api",
				Outcome:          ContainerImageIdentityTagResolved,
				Reason:           "tag resolved to one registry digest observation",
				CanonicalWrites:  1,
				IdentityStrength: "tag_observation_with_digest",
			},
		},
	}
	_, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err != nil {
		t.Fatalf("first WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	write.Decisions[0].Digest = testOtherContainerDigest
	_, err = writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err != nil {
		t.Fatalf("second WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	// Each write batches its single decision into one ExecContext call.
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}
	if got, want := rows[1].FactID, rows[0].FactID; got != want {
		t.Fatalf("fact_id changed after tag digest moved: first=%v second=%v", want, got)
	}
	if got, want := rows[1].StableFactKey, rows[0].StableFactKey; got != want {
		t.Fatalf("stable_fact_key changed after tag digest moved: first=%v second=%v", want, got)
	}
	firstPayload := unmarshalContainerImageIdentityPayload(t, rows[0].Payload)
	secondPayload := unmarshalContainerImageIdentityPayload(t, rows[1].Payload)
	if got, want := secondPayload["canonical_id"], firstPayload["canonical_id"]; got != want {
		t.Fatalf("canonical_id changed after tag digest moved: first=%v second=%v", want, got)
	}
	if got, want := secondPayload["digest"], testOtherContainerDigest; got != want {
		t.Fatalf("second payload digest = %#v, want %q", got, want)
	}
}

func TestPostgresContainerImageIdentityWriterPublishesKnownTruthLayers(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{
		DB: db,
	}
	_, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Cause:        "container image references observed",
		Decisions: []ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/api:prod",
				Digest:           testContainerDigest,
				RepositoryID:     "oci-registry://registry.example.com/team/api",
				Outcome:          ContainerImageIdentityTagResolved,
				Reason:           "tag resolved to one registry digest observation",
				CanonicalWrites:  1,
				IdentityStrength: "tag_observation_with_digest",
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	if len(rows) == 0 {
		t.Fatal("decoded rows is empty, want one batched fact row")
	}
	payload := unmarshalContainerImageIdentityPayload(t, rows[0].Payload)
	layers, ok := payload["source_layers"].([]any)
	if !ok {
		t.Fatalf("payload source_layers = %T, want []any", payload["source_layers"])
	}
	if len(layers) == 0 {
		t.Fatal("payload source_layers is empty, want known truth layers")
	}
	for _, raw := range layers {
		if _, err := truth.ParseLayer(fmt.Sprint(raw)); err != nil {
			t.Fatalf("source layer %q is not in truth model: %v", raw, err)
		}
	}
}

func unmarshalContainerImageIdentityPayload(t *testing.T, raw any) map[string]any {
	t.Helper()
	payloadBytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}
