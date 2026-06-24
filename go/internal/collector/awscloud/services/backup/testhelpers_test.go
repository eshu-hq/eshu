// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceBackup,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:backup:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        17,
		ObservedAt:          time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	vaults              []Vault
	plans               []Plan
	selections          map[string][]Selection
	recoveryPoints      map[string][]RecoveryPoint
	reportPlans         []ReportPlan
	restoreTestingPlans []RestoreTestingPlan
	frameworks          []Framework
}

func (c fakeClient) ListBackupVaults(context.Context) ([]Vault, error) {
	return c.vaults, nil
}

func (c fakeClient) ListBackupPlans(context.Context) ([]Plan, error) {
	return c.plans, nil
}

func (c fakeClient) ListBackupSelections(_ context.Context, planID string) ([]Selection, error) {
	return c.selections[planID], nil
}

func (c fakeClient) ListRecoveryPoints(_ context.Context, vaultName string) ([]RecoveryPoint, error) {
	return c.recoveryPoints[vaultName], nil
}

func (c fakeClient) ListReportPlans(context.Context) ([]ReportPlan, error) {
	return c.reportPlans, nil
}

func (c fakeClient) ListRestoreTestingPlans(context.Context) ([]RestoreTestingPlan, error) {
	return c.restoreTestingPlans, nil
}

func (c fakeClient) ListFrameworks(context.Context) ([]Framework, error) {
	return c.frameworks, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %d envelopes", relationshipType, len(envelopes))
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func int64Ptr(v int64) *int64 {
	return &v
}
