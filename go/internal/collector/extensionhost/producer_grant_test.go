// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// TestNewSourceAcceptsGrantedCoreFactKind is the #6709 slice-2 TDD red test:
// a granted core-owned declaration must survive Source construction so an
// authorized first-party producer can activate. Without grant-aware
// construction the install-time grant from slice 1 admits components that
// can never run.
func TestNewSourceAcceptsGrantedCoreFactKind(t *testing.T) {
	t.Parallel()

	manifest := testManifest()
	manifest.Spec.EmittedFacts[0].Kind = "aws_resource"
	grants := []component.ProducerGrant{{
		ProducerID:     manifest.Metadata.ID,
		Version:        manifest.Metadata.Version,
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "repo",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}}
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		Grants:              grants,
	})
	if err != nil {
		t.Fatalf("NewSource(granted core kind) error = %v, want nil", err)
	}
	if source == nil {
		t.Fatal("NewSource(granted core kind) = nil, want source")
	}
}

// grantedCoreKindManifest returns a test manifest declaring a core-owned
// kind plus a live grant covering it, for emission-path tests.
func grantedCoreKindManifest() (component.Manifest, []component.ProducerGrant) {
	manifest := testManifest()
	manifest.Spec.EmittedFacts[0].Kind = "aws_resource"
	grants := []component.ProducerGrant{{
		ProducerID:     manifest.Metadata.ID,
		Version:        manifest.Metadata.Version,
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "repo",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}}
	return manifest, grants
}

// grantedCoreKindResult returns a complete result emitting one core-owned
// fact that satisfies the contract for grantedCoreKindManifest.
func grantedCoreKindResult(item workflow.WorkItem) sdkcollector.Result {
	fact := testSDKFact(item)
	fact.Kind = "aws_resource"
	fact.SchemaVersion = "1.0.0"
	return completeResult(item, fact)
}

// TestSourceRejectsEmissionAfterGrantRevoked is the #6709 slice-3 TDD red
// test: grants are rechecked on every emission, so revoking the recorded
// grant after activation fails the next result closed with the actionable
// core-owned error instead of emitting under a dead authorization.
func TestSourceRejectsEmissionAfterGrantRevoked(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()
	revoked := grants[0]
	revoked.Revoked = true
	item := testWorkItem()
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{result: grantedCoreKindResult(item)},
		Clock:               testObservedAt,
		Grants:              grants,
		LiveGrants:          func() []component.ProducerGrant { return []component.ProducerGrant{revoked} },
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal grant-revocation error")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	if got := len(collectFacts(t, collected)); got != 0 {
		t.Fatalf("facts after revocation = %d, want 0", got)
	}
	assertFailure(t, err, FailureClassInvalidResult, true)
	if !strings.Contains(err.Error(), "aws_resource") || !strings.Contains(err.Error(), "grant") {
		t.Fatalf("NextClaimed() error = %v, want core-owned kind naming its grant", err)
	}
}

// TestSourceRejectsEmissionAfterGrantExpiry mirrors the revocation proof
// for the time bound: an expired grant fails the next result closed even
// though it authorized construction.
func TestSourceRejectsEmissionAfterGrantExpiry(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()
	expired := grants[0]
	expired.ExpiresAt = testObservedAt().Add(-time.Hour).UTC()
	live := []component.ProducerGrant{expired}
	item := testWorkItem()
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{result: grantedCoreKindResult(item)},
		Clock:               testObservedAt,
		Grants:              grants,
		LiveGrants:          func() []component.ProducerGrant { return live },
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal grant-expiry error")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	if got := len(collectFacts(t, collected)); got != 0 {
		t.Fatalf("facts after expiry = %d, want 0", got)
	}
	assertFailure(t, err, FailureClassInvalidResult, true)
	if !strings.Contains(err.Error(), "aws_resource") || !strings.Contains(err.Error(), "grant") {
		t.Fatalf("NextClaimed() error = %v, want core-owned kind naming its grant", err)
	}
}

// TestSourceRevokedGrantTurnsRetryTerminal proves the recheck runs before
// result-state handling: a revoked grant fails the result terminal even
// when the extension asks to retry, instead of retrying forever under a
// dead authorization.
func TestSourceRevokedGrantTurnsRetryTerminal(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()
	revoked := grants[0]
	revoked.Revoked = true
	item := testWorkItem()
	result := grantedCoreKindResult(item)
	result.State = sdkcollector.ResultRetryable
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{result: result},
		Clock:               testObservedAt,
		Grants:              grants,
		LiveGrants:          func() []component.ProducerGrant { return []component.ProducerGrant{revoked} },
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal grant error")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	assertFailure(t, err, FailureClassInvalidResult, true)
}

// TestSourceAcceptsEmissionWithLiveGrant proves the recheck passes while
// the grant stays live: authorization is continuous, not just checked at
// construction.
func TestSourceAcceptsEmissionWithLiveGrant(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()
	item := testWorkItem()
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{result: grantedCoreKindResult(item)},
		Clock:               testObservedAt,
		Grants:              grants,
		LiveGrants:          func() []component.ProducerGrant { return grants },
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got := len(collectFacts(t, collected)); got != 1 {
		t.Fatalf("facts with live grant = %d, want 1", got)
	}
}

// TestSourceGrantedEmissionStillEnforcesGenerationFencing proves a live
// grant does not weaken claim identity: a granted producer emitting under
// another generation still fails terminal with identity mismatch.
func TestSourceGrantedEmissionStillEnforcesGenerationFencing(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()
	item := testWorkItem()
	result := grantedCoreKindResult(item)
	result.Claim.GenerationID = "other-generation"
	result.Generation.ID = "other-generation"
	for i := range result.Facts {
		result.Facts[i].SourceRef.GenerationID = "other-generation"
	}
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{result: result},
		Clock:               testObservedAt,
		Grants:              grants,
		LiveGrants:          func() []component.ProducerGrant { return grants },
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal identity mismatch")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	if got := len(collectFacts(t, collected)); got != 0 {
		t.Fatalf("facts after fencing violation = %d, want 0", got)
	}
	assertFailure(t, err, FailureClassIdentityMismatch, true)
}

// TestNewSourceRejectsUngrantedCoreFactKind locks the fail-closed default:
// a core-owned declaration without a live grant still fails construction
// with the actionable core-owned error.
func TestNewSourceRejectsUngrantedCoreFactKind(t *testing.T) {
	t.Parallel()

	manifest := testManifest()
	manifest.Spec.EmittedFacts[0].Kind = "aws_resource"
	_, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
	})
	if err == nil {
		t.Fatal("NewSource(ungranted core kind) error = nil, want core-owned rejection")
	}
	if !strings.Contains(err.Error(), "aws_resource") || !strings.Contains(err.Error(), "core-owned") {
		t.Fatalf("NewSource() error = %v, want actionable core-owned fact-kind error", err)
	}
}
