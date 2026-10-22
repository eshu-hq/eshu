// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestIdenticalScansShareFreshnessHintAndSkip proves two scans over identical
// AWS state share one non-empty freshness hint, so the store can skip the
// second generation instead of minting per tick (#8003). The ticks use
// different generation IDs to prove the hint covers content, not identity.
func TestIdenticalScansShareFreshnessHintAndSkip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	first := collectAWSTick(t, now, "aws-generation-1", awsRoleObservation("active", map[string]string{"team": "api"}))
	second := collectAWSTick(t, now.Add(12*time.Hour), "aws-generation-2", awsRoleObservation("active", map[string]string{"team": "api"}))

	if strings.TrimSpace(first.Generation.FreshnessHint) == "" {
		t.Fatal("first FreshnessHint is blank, want stable content digest")
	}
	if got, want := second.Generation.FreshnessHint, first.Generation.FreshnessHint; got != want {
		t.Fatalf("FreshnessHint changed for unchanged AWS state: got %q, want %q", got, want)
	}
	candidate := backfill.UnchangedGenerationCandidate{
		GenerationID:  "aws-generation-1",
		FreshnessHint: first.Generation.FreshnessHint,
		Status:        scope.GenerationStatusActive,
	}
	action, _ := backfill.DecideUnchangedGeneration(second.Generation, candidate, true)
	if action != backfill.UnchangedActionSkip {
		t.Fatalf("DecideUnchangedGeneration() = %d, want skip (%d)", action, backfill.UnchangedActionSkip)
	}
}

// TestMutatedStateOrTagsStoreNewGeneration proves an identity-preserving
// drift — the same resource with new state or tags — mints a new generation.
// An identity-only hint would mask exactly this drift (#8003).
func TestMutatedStateOrTagsStoreNewGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		state      string
		tags       map[string]string
		attributes map[string]any
	}{
		{name: "state", state: "suspended", tags: map[string]string{"team": "api"}},
		{name: "tags", state: "active", tags: map[string]string{"team": "api", "cost-center": "42"}},
		{name: "attributes", state: "active", tags: map[string]string{"team": "api"}, attributes: map[string]any{"max_session_duration": 7200}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			first := collectAWSTick(t, now, "aws-generation-1", awsRoleObservation("active", map[string]string{"team": "api"}))
			second := collectAWSTick(t, now.Add(12*time.Hour), "aws-generation-2", awsRoleAttributesObservation(tt.state, tt.tags, tt.attributes))

			if first.Generation.FreshnessHint == second.Generation.FreshnessHint {
				t.Fatalf("FreshnessHint did not change after %s drift: %q", tt.name, first.Generation.FreshnessHint)
			}
			candidate := backfill.UnchangedGenerationCandidate{
				GenerationID:  "aws-generation-1",
				FreshnessHint: first.Generation.FreshnessHint,
				Status:        scope.GenerationStatusActive,
			}
			action, _ := backfill.DecideUnchangedGeneration(second.Generation, candidate, true)
			if action != backfill.UnchangedActionStore {
				t.Fatalf("DecideUnchangedGeneration() = %d, want store (%d)", action, backfill.UnchangedActionStore)
			}
		})
	}
}

// TestAWSFreshnessHintCanonicalizesNestedAttributeMaps proves nested
// attribute maps hash identically regardless of Go map iteration order, so
// the hint stays stable when the scanner rebuilds the same attributes
// (#8003). List order stays significant and is pinned only in prose: API
// list reorderings mint rather than risk masking drift.
func TestAWSFreshnessHintCanonicalizesNestedAttributeMaps(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	forward := awsRoleAttributesObservation("active", map[string]string{"team": "api"}, map[string]any{
		"outer":  "1",
		"nested": map[string]any{"x": "1", "y": "2", "deep": map[string]any{"b": "2", "a": "1"}},
	})
	reversed := awsRoleAttributesObservation("active", map[string]string{"team": "api"}, map[string]any{
		"nested": map[string]any{"deep": map[string]any{"a": "1", "b": "2"}, "y": "2", "x": "1"},
		"outer":  "1",
	})
	first := collectAWSTick(t, now, "aws-generation-1", forward)
	second := collectAWSTick(t, now, "aws-generation-2", reversed)
	if strings.TrimSpace(first.Generation.FreshnessHint) == "" {
		t.Fatal("first FreshnessHint is blank, want stable content digest")
	}
	if got, want := second.Generation.FreshnessHint, first.Generation.FreshnessHint; got != want {
		t.Fatalf("FreshnessHint changed for identical nested attributes: got %q, want %q", got, want)
	}
}

// TestCredentialFailureCarriesNoFreshnessHint pins the #8003 decision to leave
// the credential-failure path hint-less: a generation that observed no
// provider truth must always store, never skip.
func TestCredentialFailureCarriesNoFreshnessHint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	item := awsWorkItem(now)
	source := ClaimedSource{
		Config: Config{
			CollectorInstanceID: item.CollectorInstanceID,
			Targets: []TargetScope{{
				AccountID:       "123456789012",
				AllowedRegions:  []string{"us-east-1"},
				AllowedServices: []string{catalog.ServiceIAM},
				Credentials: CredentialConfig{
					Mode: CredentialModeCentralAssumeRole,
				},
			}},
		},
		Credentials: &stubCredentialProvider{err: errors.New("sts denied")},
		Scanners:    &stubScannerFactory{scanner: stubScanner{}},
		Clock:       func() time.Time { return now },
	}
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got := strings.TrimSpace(collected.Generation.FreshnessHint); got != "" {
		t.Fatalf("FreshnessHint = %q, want blank on the credential-failure path", got)
	}
}

func collectAWSTick(
	t *testing.T,
	now time.Time,
	generationID string,
	observation awscloud.ResourceObservation,
) collectedAWSTick {
	t.Helper()

	observation.Boundary.GenerationID = generationID
	envelope, err := awscloud.NewResourceEnvelope(observation)
	if err != nil {
		t.Fatalf("NewResourceEnvelope() error = %v, want nil", err)
	}
	item := awsWorkItem(now)
	item.GenerationID = generationID
	item.SourceRunID = generationID
	item.WorkItemID = "aws:collector-1:run-1:123456789012:us-east-1:iam:" + generationID
	source := ClaimedSource{
		Config: Config{
			CollectorInstanceID: item.CollectorInstanceID,
			Targets: []TargetScope{{
				AccountID:       "123456789012",
				AllowedRegions:  []string{"us-east-1"},
				AllowedServices: []string{catalog.ServiceIAM},
				Credentials: CredentialConfig{
					Mode: CredentialModeLocalWorkloadIdentity,
				},
			}},
		},
		Credentials: &stubCredentialProvider{lease: &stubCredentialLease{}},
		Scanners:    &stubScannerFactory{scanner: stubScanner{envelopes: []facts.Envelope{envelope}}},
		Clock:       func() time.Time { return now },
	}
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	drainFacts(t, collected.Facts)
	return collectedAWSTick{Generation: collected.Generation}
}

func awsRoleObservation(state string, tags map[string]string) awscloud.ResourceObservation {
	return awsRoleAttributesObservation(state, tags, nil)
}

func awsRoleAttributesObservation(
	state string,
	tags map[string]string,
	attributes map[string]any,
) awscloud.ResourceObservation {
	return awscloud.ResourceObservation{
		Boundary: awscloud.Boundary{
			AccountID:           "123456789012",
			Region:              "us-east-1",
			ServiceKind:         catalog.ServiceIAM,
			ScopeID:             "aws:123456789012:us-east-1:iam",
			CollectorInstanceID: "collector-1",
			FencingToken:        42,
			ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
		},
		ARN:          "arn:aws:iam::123456789012:role/eshu-runtime",
		ResourceType: catalog.ResourceTypeIAMRole,
		State:        state,
		Tags:         tags,
		Attributes:   attributes,
	}
}

type collectedAWSTick struct {
	Generation scope.ScopeGeneration
}
