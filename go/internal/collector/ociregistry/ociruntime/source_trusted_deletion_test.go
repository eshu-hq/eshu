// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimedSourceMapsAllowEmptySuccessorToTrustedDeletion proves the escape
// hatch wiring: only a target that explicitly attests its deletions carries
// the trusted-deletion signal the store backstop honors. The emptied scope
// below scans zero tags, so the generation holds exactly one envelope (the
// repository observation) and the store's sharp-shrink rule would refuse it
// without the attestation.
func TestClaimedSourceMapsAllowEmptySuccessorToTrustedDeletion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	collect := func(t *testing.T, allowEmpty bool) collector.CollectedGeneration {
		t.Helper()
		source := ClaimedSource{
			Source: Source{
				Config: Config{
					CollectorInstanceID: "oci-runtime-test",
					Targets: []TargetConfig{{
						Provider:            ociregistry.ProviderDockerHub,
						Registry:            "registry-1.docker.io",
						Repository:          "library/busybox",
						AllowEmptySuccessor: allowEmpty,
					}},
				},
				ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
					return &stubRegistryClient{}, nil
				}),
				Clock: func() time.Time { return observedAt },
			},
		}
		item := workflow.WorkItem{
			WorkItemID:          "oci-item-1",
			RunID:               "oci-run-1",
			CollectorKind:       scope.CollectorOCIRegistry,
			CollectorInstanceID: "oci-runtime-test",
			SourceSystem:        string(scope.CollectorOCIRegistry),
			ScopeID:             "oci-registry://registry-1.docker.io/library/busybox",
			AcceptanceUnitID:    "oci-registry://registry-1.docker.io/library/busybox",
			SourceRunID:         "oci-generation-1",
			GenerationID:        "oci-generation-1",
			CurrentFencingToken: 9,
			Status:              workflow.WorkItemStatusClaimed,
			CreatedAt:           observedAt,
			UpdatedAt:           observedAt,
		}
		collected, ok, err := source.NextClaimed(context.Background(), item)
		if err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("NextClaimed() ok = false, want true")
		}
		return collected
	}
	if got := collect(t, false).Generation; got.TrustedDeletion {
		t.Fatal("TrustedDeletion = true without AllowEmptySuccessor, want false")
	}
	attested := collect(t, true)
	if !attested.Generation.TrustedDeletion {
		t.Fatal("TrustedDeletion = false with AllowEmptySuccessor, want true")
	}
	if got, want := len(drainFacts(t, attested)), 1; got != want {
		t.Fatalf("emptied-scope envelopes = %d, want %d (repository observation only)", got, want)
	}
}

// TestClaimedSourceGlobalAttestationCoversUnattestedTargets proves the
// environment-level escape hatch: a globally attested collector stamps every
// claimed generation, so operators can cover an emptied estate without
// editing every target.
func TestClaimedSourceGlobalAttestationCoversUnattestedTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	collect := func(t *testing.T, globalAllowEmpty bool) scope.ScopeGeneration {
		t.Helper()
		source := ClaimedSource{
			Source: Source{
				Config: Config{
					CollectorInstanceID: "oci-runtime-test",
					AllowEmptySuccessor: globalAllowEmpty,
					Targets: []TargetConfig{{
						Provider:   ociregistry.ProviderDockerHub,
						Registry:   "registry-1.docker.io",
						Repository: "library/busybox",
					}},
				},
				ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
					return &stubRegistryClient{}, nil
				}),
				Clock: func() time.Time { return observedAt },
			},
		}
		item := workflow.WorkItem{
			WorkItemID:          "oci-item-1",
			RunID:               "oci-run-1",
			CollectorKind:       scope.CollectorOCIRegistry,
			CollectorInstanceID: "oci-runtime-test",
			SourceSystem:        string(scope.CollectorOCIRegistry),
			ScopeID:             "oci-registry://registry-1.docker.io/library/busybox",
			AcceptanceUnitID:    "oci-registry://registry-1.docker.io/library/busybox",
			SourceRunID:         "oci-generation-1",
			GenerationID:        "oci-generation-1",
			CurrentFencingToken: 9,
			Status:              workflow.WorkItemStatusClaimed,
			CreatedAt:           observedAt,
			UpdatedAt:           observedAt,
		}
		collected, ok, err := source.NextClaimed(context.Background(), item)
		if err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("NextClaimed() ok = false, want true")
		}
		return collected.Generation
	}
	if got := collect(t, false); got.TrustedDeletion {
		t.Fatal("TrustedDeletion = true without global AllowEmptySuccessor, want false")
	}
	if got := collect(t, true); !got.TrustedDeletion {
		t.Fatal("TrustedDeletion = false with global AllowEmptySuccessor, want true")
	}
}
