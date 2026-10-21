// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestClaimedSourceMapsAllowEmptySuccessorToTrustedDeletion proves the escape
// hatch wiring: only a target that explicitly attests its deletions carries
// the trusted-deletion signal the store backstop honors.
func TestClaimedSourceMapsAllowEmptySuccessorToTrustedDeletion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	collect := func(t *testing.T, allowEmpty bool) scope.ScopeGeneration {
		t.Helper()
		item := awsWorkItem(now)
		source := ClaimedSource{
			Config: Config{
				CollectorInstanceID: item.CollectorInstanceID,
				Targets: []TargetScope{{
					AccountID:           "123456789012",
					AllowedRegions:      []string{"us-east-1"},
					AllowedServices:     []string{catalog.ServiceIAM},
					AllowEmptySuccessor: allowEmpty,
					Credentials: CredentialConfig{
						Mode: CredentialModeLocalWorkloadIdentity,
					},
				}},
			},
			Credentials: &stubCredentialProvider{lease: &stubCredentialLease{}},
			Scanners:    &stubScannerFactory{scanner: stubScanner{envelopes: []facts.Envelope{}}},
			Clock:       func() time.Time { return now },
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
		t.Fatal("TrustedDeletion = true without AllowEmptySuccessor, want false")
	}
	if got := collect(t, true); !got.TrustedDeletion {
		t.Fatal("TrustedDeletion = false with AllowEmptySuccessor, want true")
	}
}

// TestClaimedSourceGlobalAttestationCoversUnattestedScopes proves the
// environment-level escape hatch: a globally attested collector stamps every
// claimed generation, so operators can cover an emptied estate without
// editing every target scope.
func TestClaimedSourceGlobalAttestationCoversUnattestedScopes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	collect := func(t *testing.T, globalAllowEmpty bool) scope.ScopeGeneration {
		t.Helper()
		item := awsWorkItem(now)
		source := ClaimedSource{
			Config: Config{
				CollectorInstanceID: item.CollectorInstanceID,
				AllowEmptySuccessor: globalAllowEmpty,
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
			Scanners:    &stubScannerFactory{scanner: stubScanner{envelopes: []facts.Envelope{}}},
			Clock:       func() time.Time { return now },
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
