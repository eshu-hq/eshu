// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestRecordSourceWalksEveryTargetTupleWithoutStores: record mode has no
// Postgres, so Checkpoints, ScanStatus and Limiter are nil; one generation
// per (account x region x service) in config order, then exhausted.
func TestRecordSourceWalksEveryTargetTupleWithoutStores(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	factory := &stubScannerFactory{scanner: stubScanner{envelopes: []facts.Envelope{{
		FactKind: facts.AWSResourceFactKind, StableFactKey: "resource-1", SchemaVersion: facts.AWSResourceSchemaVersion,
		CollectorKind: aws.CollectorKind, FencingToken: 1, SourceConfidence: facts.SourceConfidenceReported,
	}}}}
	provider := &stubCredentialProvider{lease: &stubCredentialLease{}}
	source := &RecordSource{Claimed: ClaimedSource{
		Config: Config{
			CollectorInstanceID: "collector-1",
			Targets: []TargetScope{
				{
					AccountID: "123456789012", AllowedRegions: []string{"us-east-1", "eu-west-1"},
					AllowedServices: []string{aws.ServiceIAM, aws.ServiceECR},
					Credentials:     CredentialConfig{Mode: CredentialModeLocalWorkloadIdentity},
				},
				{
					AccountID: "000000000000", AllowedRegions: []string{"us-east-1"},
					AllowedServices: []string{aws.ServiceIAM},
					Credentials:     CredentialConfig{Mode: CredentialModeLocalWorkloadIdentity},
				},
			},
		},
		Credentials: provider,
		Scanners:    factory,
		Clock:       func() time.Time { return now },
		// Checkpoints, ScanStatus and Limiter deliberately nil.
	}}
	want := []string{
		"aws:123456789012:us-east-1:iam", "aws:123456789012:us-east-1:ecr",
		"aws:123456789012:eu-west-1:iam", "aws:123456789012:eu-west-1:ecr",
		"aws:000000000000:us-east-1:iam",
	}
	var got []string
	for {
		gen, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if !ok {
			break
		}
		got = append(got, gen.Scope.ScopeID)
		if gen.Scope.Metadata["account_id"] == "" || gen.Scope.Metadata["service_kind"] == "" {
			t.Errorf("scope %q lost its metadata", gen.Scope.ScopeID)
		}
		if n := len(drainFacts(t, gen.Facts)); n != 1 {
			t.Errorf("scope %q: %d facts, want 1", gen.Scope.ScopeID, n)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scope[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if _, ok, _ := source.Next(context.Background()); ok {
		t.Error("source yielded after exhaustion")
	}
	if factory.boundary.FencingToken != 1 || factory.boundary.CollectorInstanceID != "collector-1" {
		t.Errorf("boundary = %+v, want fencing token 1 under collector-1", factory.boundary)
	}
	if provider.target.Credentials.Mode != CredentialModeLocalWorkloadIdentity {
		t.Errorf("credential mode not forwarded to the provider")
	}
}

// TestRecordSourceRequiresInstanceAndTargets mirrors ClaimedSource.validate.
func TestRecordSourceRequiresInstanceAndTargets(t *testing.T) {
	source := &RecordSource{Claimed: ClaimedSource{
		Credentials: &stubCredentialProvider{lease: &stubCredentialLease{}},
		Scanners:    &stubScannerFactory{scanner: stubScanner{}},
	}}
	if _, _, err := source.Next(context.Background()); err == nil {
		t.Fatal("record source without an instance id or targets = nil error")
	}
}
