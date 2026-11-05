// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	remoteclaim "github.com/eshu-hq/eshu/go/internal/collector/extensionhost/remote/claim"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// parityRow is one hostile grant state replayed through both the
// worker-built cutover and the in-tree emission truth. The cutover must
// agree with the in-tree evaluation row for row: same allow/deny, same
// closed reason, same observed decision.
type parityRow struct {
	name string
	// arrange records the row's grants; nil leaves the registry empty.
	arrange func(t *testing.T, registry component.Registry)
	// unreadable forces the grant-set read to fail instead of arranging.
	unreadable bool
	kind       string
	schema     string

	wantAllowed      bool
	wantReason       component.GrantReason
	wantObservations int
}

const (
	parityHolderID      = cutoverObserverComponentID
	parityHolderVersion = cutoverObserverVersion
	parityCoreKind      = cutoverObserverCoreKind
	parityCoreSchema    = cutoverObserverSchema
	parityScope         = cutoverObserverScope
	parityNonCoreKind   = "scorecard"
)

func parityLiveGrant() component.ProducerGrant {
	return component.ProducerGrant{
		ProducerID:     parityHolderID,
		Version:        parityHolderVersion,
		Kind:           parityCoreKind,
		SchemaVersions: []string{parityCoreSchema},
		Scope:          parityScope,
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
}

func parityRows() []parityRow {
	record := func(g component.ProducerGrant) func(*testing.T, component.Registry) {
		return func(t *testing.T, registry component.Registry) {
			t.Helper()
			if err := registry.RecordGrant(g); err != nil {
				t.Fatalf("RecordGrant() error = %v, want nil", err)
			}
		}
	}
	live := parityLiveGrant()
	revoked := parityLiveGrant()
	revoked.Revoked = true
	expired := parityLiveGrant()
	expired.ExpiresAt = time.Now().Add(-time.Hour).UTC()
	wrongScope := parityLiveGrant()
	wrongScope.Scope = "somewhere-else"
	wrongSchema := parityLiveGrant()
	wrongSchema.SchemaVersions = []string{"9.9.9"}
	impostor := parityLiveGrant()
	impostor.ProducerID = "dev.eshu.examples.impostor"
	return []parityRow{
		{
			name: "live grant allows", arrange: record(live),
			kind: parityCoreKind, schema: parityCoreSchema,
			wantAllowed: true, wantReason: component.GrantReasonGranted, wantObservations: 1,
		},
		{
			name: "revoked grant denies", arrange: record(revoked),
			kind: parityCoreKind, schema: parityCoreSchema,
			wantAllowed: false, wantReason: component.GrantReasonRevoked, wantObservations: 1,
		},
		{
			name: "expired grant denies", arrange: record(expired),
			kind: parityCoreKind, schema: parityCoreSchema,
			wantAllowed: false, wantReason: component.GrantReasonExpired, wantObservations: 1,
		},
		{
			name: "wrong scope denies", arrange: record(wrongScope),
			kind: parityCoreKind, schema: parityCoreSchema,
			wantAllowed: false, wantReason: component.GrantReasonScopeMismatch, wantObservations: 1,
		},
		{
			name: "uncovered schema denies", arrange: record(wrongSchema),
			kind: parityCoreKind, schema: parityCoreSchema,
			wantAllowed: false, wantReason: component.GrantReasonSchemaNotCovered, wantObservations: 1,
		},
		{
			name: "absent grant denies", arrange: nil,
			kind: parityCoreKind, schema: parityCoreSchema,
			wantAllowed: false, wantReason: component.GrantReasonNoMatchingGrant, wantObservations: 1,
		},
		{
			name: "impostor grant denies", arrange: record(impostor),
			kind: parityCoreKind, schema: parityCoreSchema,
			wantAllowed: false, wantReason: component.GrantReasonNoMatchingGrant, wantObservations: 1,
		},
		{
			name: "unreadable set denies closed", unreadable: true,
			kind: parityCoreKind, schema: parityCoreSchema,
			wantAllowed: false, wantReason: component.GrantReasonGrantsUnreadable, wantObservations: 1,
		},
		{
			name: "non-core kind needs no grant", arrange: nil,
			kind: parityNonCoreKind, schema: parityCoreSchema,
			wantAllowed: true, wantReason: component.GrantReasonGranted, wantObservations: 0,
		},
	}
}

// parityCutover builds the worker-built cutover over a registry home
// holding exactly this row's grants, mirroring buildRemoteCutover's
// production wiring (pinned holder, registry reader, bound observer).
func parityCutover(t *testing.T, home string, observer *recordingGrantObserver) *remoteclaim.Cutover {
	t.Helper()

	plan := cutoverPlan{
		enabled:         true,
		remoteURL:       "http://eshu-workflow-claim-plane:8080",
		producerID:      parityHolderID,
		producerVersion: parityHolderVersion,
		scopes:          []string{parityScope},
	}
	cutover, err := buildRemoteCutover(plan, home, observer)
	if err != nil {
		t.Fatalf("buildRemoteCutover() error = %v, want nil", err)
	}
	return cutover
}

// parityInTree evaluates the row through the same exported predicates with
// the same inputs the in-tree Source.validateGrantCoverage passes: registry
// grants, manifest holder identity and scopes, emission kind and schema.
// A read error denies core kinds closed, mirroring the in-tree closure.
func parityInTree(grants []component.ProducerGrant, readErr error, kind, schema string, now time.Time) (bool, component.GrantReason, bool) {
	if readErr != nil {
		grants = nil
	}
	allowed, governed := component.EvaluateEmission(
		grants, parityHolderID, parityHolderVersion, kind, schema, []string{parityScope}, now)
	if allowed {
		return true, component.GrantReasonGranted, governed
	}
	reason := component.GrantReasonGrantsUnreadable
	if readErr == nil {
		reason = component.ClassifyEmission(
			grants, parityHolderID, parityHolderVersion, kind, schema, []string{parityScope}, now)
	}
	return false, reason, governed
}

// TestCutoverEmissionParityWithInTree proves the worker-built cutover agrees
// with the in-tree emission truth row for row across the hostile grant
// matrix, and that every governed decision is observed with the same
// allow/deny, reason, stage, and holder the in-tree observer would record
// (eshu-hq/eshu#8877 leaf 3).
func TestCutoverEmissionParityWithInTree(t *testing.T) {
	t.Parallel()

	for _, row := range parityRows() {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			if row.unreadable {
				// A regular file where the registry expects a directory
				// fails the grant-set load.
				home = filepath.Join(home, "not-a-registry-home")
				if err := os.WriteFile(home, []byte("hostile"), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v, want nil", err)
				}
			} else if row.arrange != nil {
				row.arrange(t, component.NewRegistry(home))
			}
			now := time.Now().UTC()
			observer := &recordingGrantObserver{}
			cutover := parityCutover(t, home, observer)
			cutoverErr := cutover.AuthorizeEmission(context.Background(), row.kind, row.schema)

			grants, readErr := component.NewRegistry(home).ProducerGrants()
			inAllowed, inReason, _ := parityInTree(grants, readErr, row.kind, row.schema, now)

			cutAllowed := cutoverErr == nil
			var cutReason component.GrantReason
			if deny, ok := cutoverErr.(*remoteclaim.DenyError); ok {
				cutReason = deny.Reason
			} else if cutAllowed {
				cutReason = component.GrantReasonGranted
			} else {
				t.Fatalf("AuthorizeEmission() error = %v (%T), want nil or *DenyError", cutoverErr, cutoverErr)
			}
			if cutAllowed != inAllowed || cutReason != inReason {
				t.Fatalf("cutover=(%t,%q) in-tree=(%t,%q): paths disagree on %q",
					cutAllowed, cutReason, inAllowed, inReason, row.name)
			}
			if cutAllowed != row.wantAllowed || cutReason != row.wantReason {
				t.Fatalf("outcome=(%t,%q) want=(%t,%q) on %q",
					cutAllowed, cutReason, row.wantAllowed, row.wantReason, row.name)
			}
			got := observer.snapshot()
			if len(got) != row.wantObservations {
				t.Fatalf("observed decisions = %d, want %d on %q", len(got), row.wantObservations, row.name)
			}
			for _, decision := range got {
				if decision.Allowed != row.wantAllowed || decision.Reason != row.wantReason {
					t.Errorf("observed=(%t,%q) want=(%t,%q) on %q",
						decision.Allowed, decision.Reason, row.wantAllowed, row.wantReason, row.name)
				}
				if decision.Stage != component.GrantStageEmission {
					t.Errorf("observed stage = %q, want emission on %q", decision.Stage, row.name)
				}
				if decision.ProducerID != parityHolderID || decision.Version != parityHolderVersion {
					t.Errorf("observed holder = %q %q, want the pinned holder on %q",
						decision.ProducerID, decision.Version, row.name)
				}
				if decision.Kind != row.kind {
					t.Errorf("observed kind = %q, want %q", decision.Kind, row.kind)
				}
			}
		})
	}
}
