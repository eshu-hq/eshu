// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func presenceIntent(domain Domain) Intent {
	return Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       domain,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func TestAWSResourceMaterializationRecordsEndpointPresenceWhenEnabled(t *testing.T) {
	t.Parallel()

	presence := &recordingPresenceWriter{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_ec2_vpc", "resource_id": "vpc-123",
		}),
		awsResourceEnvelope(map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_iam_role", "resource_id": "arn:aws:iam::111122223333:role/app",
			"arn": "arn:aws:iam::111122223333:role/app",
		}),
	}}
	handler := AWSResourceMaterializationHandler{FactLoader: loader, NodeWriter: &recordingCloudResourceNodeWriter{}, PresenceWriter: presence}

	if _, err := handler.Handle(context.Background(), presenceIntent(DomainAWSResourceMaterialization)); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(presence.upserts) != 1 {
		t.Fatalf("presence upsert calls = %d, want 1", len(presence.upserts))
	}
	rows := presence.upserts[0]
	if len(rows) != 2 {
		t.Fatalf("presence rows = %d, want 2 (one per committed node uid)", len(rows))
	}
	for _, r := range rows {
		if r.Keyspace != GraphProjectionKeyspaceCloudResourceUID || r.ScopeID != "scope-1" || r.UID == "" {
			t.Fatalf("bad presence row: %+v", r)
		}
	}
}

func TestAWSResourceMaterializationNoPresenceWhenWriterNil(t *testing.T) {
	t.Parallel()

	// The default (feature-off) path: a nil PresenceWriter must not change the
	// materializer and must perform no presence work.
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_ec2_vpc", "resource_id": "vpc-123",
		}),
	}}
	writer := &recordingCloudResourceNodeWriter{}
	handler := AWSResourceMaterializationHandler{FactLoader: loader, NodeWriter: writer} // PresenceWriter nil

	if _, err := handler.Handle(context.Background(), presenceIntent(DomainAWSResourceMaterialization)); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("node writer calls = %d, want 1 (off path unchanged)", writer.calls)
	}
}

func TestKubernetesWorkloadMaterializationRecordsEndpointPresenceWhenEnabled(t *testing.T) {
	t.Parallel()

	presence := &recordingPresenceWriter{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "checkout")),
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-b", "ledger")),
	}}
	handler := KubernetesWorkloadMaterializationHandler{FactLoader: loader, NodeWriter: &recordingKubernetesWorkloadNodeWriter{}, PresenceWriter: presence}

	if _, err := handler.Handle(context.Background(), presenceIntent(DomainKubernetesWorkloadMaterialization)); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(presence.upserts) != 1 {
		t.Fatalf("presence upsert calls = %d, want 1", len(presence.upserts))
	}
	for _, r := range presence.upserts[0] {
		if r.Keyspace != GraphProjectionKeyspaceKubernetesWorkloadUID || r.ScopeID != "scope-1" || r.UID == "" {
			t.Fatalf("bad presence row: %+v", r)
		}
	}
}
