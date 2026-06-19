package awsruntime_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fixtureConfig returns a deterministic two-resource, one-relationship AWS
// estate config with no credentials and no derived scope/generation ids so the
// source must derive both.
func fixtureConfig() awsruntime.FixtureConfig {
	return awsruntime.FixtureConfig{
		CollectorInstanceID: "aws-fixture-instance",
		Scopes: []awsruntime.FixtureScope{
			{
				AccountID:   "111122223333",
				Region:      "us-east-1",
				ServiceKind: "s3",
				Resources: []awsruntime.FixtureResource{
					{
						ARN:          "arn:aws:s3:::eshu-fixture-managed",
						ResourceID:   "eshu-fixture-managed",
						ResourceType: "aws_s3_bucket",
						Name:         "eshu-fixture-managed",
						State:        "available",
						Tags:         map[string]string{"managed": "true"},
					},
					{
						ARN:          "arn:aws:s3:::eshu-fixture-unmanaged",
						ResourceID:   "eshu-fixture-unmanaged",
						ResourceType: "aws_s3_bucket",
						Name:         "eshu-fixture-unmanaged",
						State:        "available",
					},
				},
				Relationships: []awsruntime.FixtureRelationship{
					{
						RelationshipType: "contains",
						SourceResourceID: "eshu-fixture-managed",
						SourceARN:        "arn:aws:s3:::eshu-fixture-managed",
						TargetResourceID: "eshu-fixture-unmanaged",
						TargetARN:        "arn:aws:s3:::eshu-fixture-unmanaged",
						TargetType:       "aws_s3_bucket",
					},
				},
			},
		},
	}
}

// TestFixtureSourceNextEmitsExpectedFacts proves a single Next yields the
// configured aws_resource and aws_relationship envelopes with the derived
// scope and generation identity and the expected payload, with no credentials.
func TestFixtureSourceNextEmitsExpectedFacts(t *testing.T) {
	t.Parallel()

	source := &awsruntime.FixtureSource{Config: fixtureConfig()}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("Next() ok = false, want true on first scope")
	}

	wantScopeID := "aws:111122223333:us-east-1:s3"
	if collected.Scope.ScopeID != wantScopeID {
		t.Fatalf("scope id = %q, want %q", collected.Scope.ScopeID, wantScopeID)
	}
	if collected.Scope.CollectorKind != scope.CollectorAWS {
		t.Fatalf("collector kind = %q, want %q", collected.Scope.CollectorKind, scope.CollectorAWS)
	}
	if collected.Scope.ScopeKind != scope.KindRegion {
		t.Fatalf("scope kind = %q, want %q", collected.Scope.ScopeKind, scope.KindRegion)
	}
	if collected.Scope.SourceSystem != awscloud.CollectorKind {
		t.Fatalf("source system = %q, want %q", collected.Scope.SourceSystem, awscloud.CollectorKind)
	}
	if collected.Generation.ScopeID != wantScopeID {
		t.Fatalf("generation scope id = %q, want %q", collected.Generation.ScopeID, wantScopeID)
	}
	if collected.Generation.GenerationID == "" {
		t.Fatalf("generation id is empty, want a derived stable id")
	}
	if collected.Generation.Status != scope.GenerationStatusPending {
		t.Fatalf("generation status = %q, want %q", collected.Generation.Status, scope.GenerationStatusPending)
	}

	resources := 0
	relationships := 0
	sawUnmanaged := false
	for env := range collected.Facts {
		if env.ScopeID != wantScopeID {
			t.Fatalf("envelope scope id = %q, want %q", env.ScopeID, wantScopeID)
		}
		if env.GenerationID != collected.Generation.GenerationID {
			t.Fatalf("envelope generation id = %q, want %q", env.GenerationID, collected.Generation.GenerationID)
		}
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources++
			if env.Payload["resource_type"] != "aws_s3_bucket" {
				t.Fatalf("resource_type = %v, want aws_s3_bucket", env.Payload["resource_type"])
			}
			if env.Payload["arn"] == "arn:aws:s3:::eshu-fixture-unmanaged" {
				sawUnmanaged = true
			}
		case facts.AWSRelationshipFactKind:
			relationships++
			if env.Payload["relationship_type"] != "contains" {
				t.Fatalf("relationship_type = %v, want contains", env.Payload["relationship_type"])
			}
		default:
			t.Fatalf("unexpected fact kind %q", env.FactKind)
		}
	}
	if resources != 2 {
		t.Fatalf("resource fact count = %d, want 2", resources)
	}
	if relationships != 1 {
		t.Fatalf("relationship fact count = %d, want 1", relationships)
	}
	if !sawUnmanaged {
		t.Fatalf("did not observe the orphaned arn:aws:s3:::eshu-fixture-unmanaged resource fact")
	}
}

// TestFixtureSourceDrainsThenRestarts proves the second Next reports the batch
// is drained (ok=false) and the following Next restarts the batch so the
// collector.Service poll loop re-emits the same facts on the next poll.
func TestFixtureSourceDrainsThenRestarts(t *testing.T) {
	t.Parallel()

	source := &awsruntime.FixtureSource{Config: fixtureConfig()}

	if _, ok, err := source.Next(context.Background()); err != nil || !ok {
		t.Fatalf("first Next ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if _, ok, err := source.Next(context.Background()); err != nil || ok {
		t.Fatalf("second Next ok=%v err=%v, want ok=false err=nil (batch drained)", ok, err)
	}
	if _, ok, err := source.Next(context.Background()); err != nil || !ok {
		t.Fatalf("third Next ok=%v err=%v, want ok=true err=nil (batch restarted)", ok, err)
	}
}

// TestFixtureSourceGenerationsAreDeterministic proves two fresh sources with
// identical config derive the same generation id, so re-ingest is idempotent
// and CI is reproducible.
func TestFixtureSourceGenerationsAreDeterministic(t *testing.T) {
	t.Parallel()

	first := &awsruntime.FixtureSource{Config: fixtureConfig()}
	second := &awsruntime.FixtureSource{Config: fixtureConfig()}

	firstCollected, _, err := first.Next(context.Background())
	if err != nil {
		t.Fatalf("first Next error = %v", err)
	}
	secondCollected, _, err := second.Next(context.Background())
	if err != nil {
		t.Fatalf("second Next error = %v", err)
	}
	if firstCollected.Generation.GenerationID != secondCollected.Generation.GenerationID {
		t.Fatalf("generation ids differ: %q vs %q",
			firstCollected.Generation.GenerationID, secondCollected.Generation.GenerationID)
	}

	firstIDs := envelopeFactIDs(t, firstCollected.Facts)
	secondIDs := envelopeFactIDs(t, secondCollected.Facts)
	if len(firstIDs) != len(secondIDs) {
		t.Fatalf("fact id count differs: %d vs %d", len(firstIDs), len(secondIDs))
	}
	for i := range firstIDs {
		if firstIDs[i] != secondIDs[i] {
			t.Fatalf("fact id %d differs: %q vs %q", i, firstIDs[i], secondIDs[i])
		}
	}
}

func envelopeFactIDs(t *testing.T, stream <-chan facts.Envelope) []string {
	t.Helper()
	ids := make([]string, 0)
	for env := range stream {
		ids = append(ids, env.FactID)
	}
	return ids
}

// TestFixtureSourceValidateRejectsEmptyConfig proves an empty config is
// rejected rather than silently producing no facts.
func TestFixtureSourceValidateRejectsEmptyConfig(t *testing.T) {
	t.Parallel()

	source := &awsruntime.FixtureSource{Config: awsruntime.FixtureConfig{}}
	if _, _, err := source.Next(context.Background()); err == nil {
		t.Fatalf("Next() error = nil, want validation error for empty config")
	}
}
