// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

type seamRows struct{}

func (seamRows) Next() bool        { return false }
func (seamRows) Scan(...any) error { return nil }
func (seamRows) Err() error        { return nil }
func (seamRows) Close() error      { return nil }

type seamTx struct{}

func (seamTx) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
func (seamTx) QueryContext(context.Context, string, ...any) (supplychaincore.SupplyChainImpactRows, error) {
	return seamRows{}, nil
}
func (seamTx) Commit() error   { return nil }
func (seamTx) Rollback() error { return nil }

// TestRecheckHookTxToleratesUnsetSeams pins the live-test wrapper itself: a wrapper built
// with only onEnd or only beforeLock (the footprint and the 3 s proofs) must not panic
// when the recheck or the lock statement passes through it, or one live proof would
// kill the whole live test binary.
func TestRecheckHookTxToleratesUnsetSeams(t *testing.T) {
	t.Parallel()

	tx := &recheckHookTx{tx: seamTx{}, onEnd: func([]string, bool) {}}
	if _, err := tx.ExecContext(context.Background(), supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery); err != nil {
		t.Fatalf("lock statement: %v", err)
	}
	if _, err := tx.QueryContext(context.Background(), "SELECT 1 FROM unnest($1::text[], $2::text[]) AS pair(scope_id, generation_id)"); err != nil {
		t.Fatalf("recheck statement: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

type protectedSeamTx struct {
	seamTx
	authorized int
	published  int
}

func (p *protectedSeamTx) AuthorizeProtectedScanImpactResults(context.Context, supplychaincore.ProtectedScanImpactPublicationSet) error {
	p.authorized++
	return nil
}

func (p *protectedSeamTx) PublishProtectedScanImpactResults(context.Context, supplychaincore.ProtectedScanImpactPublicationSet) error {
	p.published++
	return nil
}

func TestRecheckHookTxForwardsProtectedScanImpact(t *testing.T) {
	t.Parallel()

	inner := &protectedSeamTx{}
	wrapped := &recheckHookTx{tx: inner}
	var authorizer supplychaincore.ProtectedScanImpactAuthorizer = wrapped
	var publisher supplychaincore.ProtectedScanImpactPublisher = wrapped
	if err := authorizer.AuthorizeProtectedScanImpactResults(context.Background(), supplychaincore.ProtectedScanImpactPublicationSet{}); err != nil {
		t.Fatalf("authorize forwarded: %v", err)
	}
	if err := publisher.PublishProtectedScanImpactResults(context.Background(), supplychaincore.ProtectedScanImpactPublicationSet{}); err != nil {
		t.Fatalf("publish forwarded: %v", err)
	}
	if inner.authorized != 1 || inner.published != 1 {
		t.Fatalf("forwarded counts = (authorized=%d, published=%d), want (1, 1)", inner.authorized, inner.published)
	}

	plain := &recheckHookTx{tx: seamTx{}}
	if err := plain.AuthorizeProtectedScanImpactResults(context.Background(), supplychaincore.ProtectedScanImpactPublicationSet{}); err == nil {
		t.Fatal("AuthorizeProtectedScanImpactResults on non-authorizer tx = nil, want error")
	}
	if err := plain.PublishProtectedScanImpactResults(context.Background(), supplychaincore.ProtectedScanImpactPublicationSet{}); err == nil {
		t.Fatal("PublishProtectedScanImpactResults on non-publisher tx = nil, want error")
	}
}

func TestGoEvidenceTestRepoIDProducesCanonicalGoReachabilityScopes(t *testing.T) {
	t.Parallel()

	seen := make(map[string]string)
	seeds := []string{"11327:pr:0", "11327:pr:1", "11327:pr:dormant", "11327:fp:0"}
	for round := 0; round < 40; round++ {
		seeds = append(seeds,
			fmt.Sprintf("11327:ak%02d:0", round),
			fmt.Sprintf("11327:fi%02d:0", round),
			fmt.Sprintf("11327:sup:%02d", round),
			fmt.Sprintf("11327:prot:%02d", round),
		)
	}
	for _, seed := range seeds {
		repoID := goEvidenceTestRepoID(seed)
		scopeID := scannercomponent.GoReachabilityScopePrefix + repoID
		got, ok := scannercomponent.ParseGoReachabilityScopeID(scopeID)
		if !ok || got != repoID {
			t.Fatalf("ParseGoReachabilityScopeID(%q) = (%q, %v), want (%q, true)", scopeID, got, ok, repoID)
		}
		if prev, dup := seen[repoID]; dup {
			t.Fatalf("seed %q collided with %q on %q", seed, prev, repoID)
		}
		seen[repoID] = seed
	}
}
