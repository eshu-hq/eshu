// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstateruntime_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimedSourceStampsTrustedDeletionOnlyForAttestedEmptyStates proves the
// #8329 gated escape hatch: an explicitly attested, legitimately empty state
// (snapshot only, zero body facts) carries the trusted-deletion signal the
// store backstop honors, while every other shape stays unattested even with
// the flag set.
func TestClaimedSourceStampsTrustedDeletionOnlyForAttestedEmptyStates(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	locator := "s3://tfstate-prod/services/api/terraform.tfstate"
	collectParsed := func(t *testing.T, state string, allowEmpty bool) collector.CollectedGeneration {
		t.Helper()
		key, err := redact.NewKey([]byte("runtime-redaction-key"))
		if err != nil {
			t.Fatalf("NewKey() error = %v, want nil", err)
		}
		stateKey := terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     locator,
		}
		source := tfstateruntime.ClaimedSource{
			Resolver: terraformstate.DiscoveryResolver{
				Config: terraformstate.DiscoveryConfig{
					Seeds: []terraformstate.DiscoverySeed{{
						Kind:   terraformstate.BackendS3,
						Bucket: "tfstate-prod",
						Key:    "services/api/terraform.tfstate",
						Region: "us-east-1",
					}},
				},
			},
			SourceFactory: &fakeFactory{
				source: &fakeStateSource{key: stateKey, state: state, observedAt: observedAt},
			},
			RedactionKey:        key,
			AllowEmptySuccessor: allowEmpty,
			Clock:               func() time.Time { return observedAt },
		}
		collected, ok, err := source.NextClaimed(context.Background(), matchingRuntimeItem(t, locator, observedAt))
		if err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("NextClaimed() ok = false, want true")
		}
		return collected
	}

	empty := `{"serial":17,"lineage":"lineage-123","resources":[]}`
	if got := collectParsed(t, empty, false).Generation; got.TrustedDeletion {
		t.Fatal("TrustedDeletion = true without AllowEmptySuccessor, want false")
	}
	attested := collectParsed(t, empty, true)
	if !attested.Generation.TrustedDeletion {
		t.Fatal("TrustedDeletion = false for attested empty state, want true")
	}
	if got, want := len(drainRuntimeFacts(t, attested.Facts)), 1; got != want {
		t.Fatalf("attested empty envelopes = %d, want %d (snapshot only)", got, want)
	}

	contentBearing := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_instance","name":"api","instances":[{"attributes":{"id":"i-1"}}]}
	]}`
	if got := collectParsed(t, contentBearing, true).Generation; got.TrustedDeletion {
		t.Fatal("TrustedDeletion = true for content-bearing state with AllowEmptySuccessor, want false (gated on the empty shape)")
	}
}

// TestClaimedSourceFailureGenerationsNeverAttest proves failure shapes stay
// unattested even with the flag set: a missing state, an over-ceiling state,
// and an identity-mismatched state must never carry the trusted-deletion
// signal.
func TestClaimedSourceFailureGenerationsNeverAttest(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	locator := "s3://tfstate-prod/services/api/terraform.tfstate"
	collectWithOpenErr := func(t *testing.T, openErr error) (collector.CollectedGeneration, bool) {
		t.Helper()
		key, err := redact.NewKey([]byte("runtime-redaction-key"))
		if err != nil {
			t.Fatalf("NewKey() error = %v, want nil", err)
		}
		stateKey := terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     locator,
		}
		scopeValue, err := scope.NewTerraformStateSnapshotScope("", string(stateKey.BackendKind), stateKey.Locator, nil)
		if err != nil {
			t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
		}
		candidateID, err := terraformstate.CandidatePlanningID(terraformstate.DiscoveryCandidate{
			State:  stateKey,
			Source: terraformstate.DiscoveryCandidateSourceSeed,
			Region: "us-east-1",
		})
		if err != nil {
			t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
		}
		source := tfstateruntime.ClaimedSource{
			Resolver: terraformstate.DiscoveryResolver{
				Config: terraformstate.DiscoveryConfig{
					Seeds: []terraformstate.DiscoverySeed{{
						Kind:   terraformstate.BackendS3,
						Bucket: "tfstate-prod",
						Key:    "services/api/terraform.tfstate",
						Region: "us-east-1",
					}},
				},
			},
			SourceFactory: &fakeFactory{
				source: &fakeStateSource{key: stateKey, openErr: openErr, observedAt: observedAt},
			},
			RedactionKey:        key,
			AllowEmptySuccessor: true,
			Clock:               func() time.Time { return observedAt },
		}
		item := workflow.WorkItem{
			WorkItemID:          "tfstate-work-failure",
			RunID:               "run-1",
			CollectorKind:       scope.CollectorTerraformState,
			CollectorInstanceID: "collector-tfstate-primary",
			SourceSystem:        string(scope.CollectorTerraformState),
			ScopeID:             scopeValue.ScopeID,
			AcceptanceUnitID:    "platform-infra",
			SourceRunID:         candidateID,
			GenerationID:        candidateID,
			Status:              workflow.WorkItemStatusClaimed,
			AttemptCount:        1,
			CurrentFencingToken: 42,
			CreatedAt:           observedAt,
			UpdatedAt:           observedAt,
		}
		collected, ok, err := source.NextClaimed(context.Background(), item)
		if err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		return collected, ok
	}

	for _, failure := range []struct {
		name    string
		openErr error
	}{
		{"missing", fmt.Errorf("GET <redacted>: %w", terraformstate.ErrStateMissing)},
		{"too_large", fmt.Errorf("%w: size=1024 max=512", terraformstate.ErrStateTooLarge)},
	} {
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			collected, ok := collectWithOpenErr(t, failure.openErr)
			if !ok {
				t.Fatal("NextClaimed() ok = false, want warning generation")
			}
			if collected.Generation.TrustedDeletion {
				t.Fatalf("TrustedDeletion = true for %s state with AllowEmptySuccessor, want false", failure.name)
			}
		})
	}

	t.Run("identity_mismatch", func(t *testing.T) {
		t.Parallel()
		key, err := redact.NewKey([]byte("runtime-redaction-key"))
		if err != nil {
			t.Fatalf("NewKey() error = %v, want nil", err)
		}
		stateKey := terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     locator,
		}
		source := tfstateruntime.ClaimedSource{
			Resolver: terraformstate.DiscoveryResolver{
				Config: terraformstate.DiscoveryConfig{
					Seeds: []terraformstate.DiscoverySeed{{
						Kind:   terraformstate.BackendS3,
						Bucket: "tfstate-prod",
						Key:    "services/api/terraform.tfstate",
						Region: "us-east-1",
					}},
				},
			},
			SourceFactory: &fakeFactory{
				source: &fakeStateSource{
					key:        stateKey,
					state:      `{"serial":99,"lineage":"lineage-999","resources":[]}`,
					observedAt: observedAt,
				},
			},
			RedactionKey:        key,
			AllowEmptySuccessor: true,
			Clock:               func() time.Time { return observedAt },
		}
		// The claim expects serial 17/lineage-123; the source observes
		// serial 99/lineage-999, so no generation may attest.
		_, ok, err := source.NextClaimed(context.Background(), matchingRuntimeItem(t, locator, observedAt))
		if err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		if ok {
			t.Fatal("NextClaimed() ok = true for identity-mismatched state, want false (no generation to attest)")
		}
	})
}
