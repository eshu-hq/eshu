// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errSecretsIAMWireShapeProbe is the sentinel the failing guarded
// reader below answers every read with. A non-nil error (never a
// panic, never a confident empty result) is the fail-closed proof the
// satellite-consumable constructor wires the guarded port through.
var errSecretsIAMWireShapeProbe = errors.New("secrets/IAM wire-shape probe: reader unavailable")

// errSecretsIAMWireShapeRow is a db.Row that always fails its scan.
type errSecretsIAMWireShapeRow struct{ err error }

func (r errSecretsIAMWireShapeRow) Scan(...any) error { return r.err }

// errSecretsIAMWireShapeReadStore is a guarded reader that answers
// every read with the probe error. It implements the full
// db.ReadStore surface (cursor, row, and snapshot reads), so
// constructing the admission satisfier over it proves the satellite
// path needs nothing wider than the request-scoped ReadStore handle.
type errSecretsIAMWireShapeReadStore struct{ err error }

func (s errSecretsIAMWireShapeReadStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

func (s errSecretsIAMWireShapeReadStore) QueryRowContext(context.Context, string, ...any) db.Row {
	return errSecretsIAMWireShapeRow(s)
}

func (s errSecretsIAMWireShapeReadStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, s.err
}

var _ db.ReadStore = errSecretsIAMWireShapeReadStore{}

// TestSecretsIAMReadAdmissionWireShapesAreSatelliteConsumable proves
// the wire shapes behind the #9669 pin: the production Postgres
// stores satisfy the admission jointly, and the WithReadStore
// constructor serves all five re-declared reads over nothing wider
// than the guarded ReadStore the satellite holds. Every read fails
// closed (never panics, never returns confident emptiness) when the
// guarded reader is unavailable.
func TestSecretsIAMReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	store := errSecretsIAMWireShapeReadStore{err: errSecretsIAMWireShapeProbe}
	var _ SecretsIAMReadAdmission = secretsIAMReadAdmissionWitness{
		PostgresIAMIdentityTrustChainStore:          NewPostgresIAMIdentityTrustChainStoreWithReadStore(store),
		PostgresIAMPrivilegePostureObservationStore: NewPostgresIAMPrivilegePostureObservationStoreWithReadStore(store),
		PostgresIAMSecretAccessPathStore:            NewPostgresIAMSecretAccessPathStoreWithReadStore(store),
		PostgresIAMPostureGapStore:                  NewPostgresIAMPostureGapStoreWithReadStore(store),
		PostgresIAMPostureSummaryStore:              NewPostgresIAMPostureSummaryStoreWithReadStore(store),
	}

	admission := NewReadAdmissionWithReadStore(store)
	if admission == nil {
		t.Fatal("NewReadAdmissionWithReadStore() = nil, want a SecretsIAMReadAdmission bound to the guarded reader")
	}

	ctx := context.Background()
	// Each read must fail with the guarded reader's own probe error (not
	// merely any non-nil error): that pins the satellite path to the
	// guarded ReadStore instead of an error from another cause.
	if _, err := admission.ListSecretsIAMIdentityTrustChains(ctx, IAMIdentityTrustChainFilter{ScopeID: "scope-1", Limit: 10}); !errors.Is(err, errSecretsIAMWireShapeProbe) {
		t.Errorf("ListSecretsIAMIdentityTrustChains over an unavailable reader = %v, want the guarded reader's probe error", err)
	}
	if _, err := admission.ListSecretsIAMPrivilegePostureObservations(ctx, IAMPrivilegePostureObservationFilter{ScopeID: "scope-1", Limit: 10}); !errors.Is(err, errSecretsIAMWireShapeProbe) {
		t.Errorf("ListSecretsIAMPrivilegePostureObservations over an unavailable reader = %v, want the guarded reader's probe error", err)
	}
	if _, err := admission.ListSecretsIAMSecretAccessPaths(ctx, IAMSecretAccessPathFilter{ScopeID: "scope-1", Limit: 10}); !errors.Is(err, errSecretsIAMWireShapeProbe) {
		t.Errorf("ListSecretsIAMSecretAccessPaths over an unavailable reader = %v, want the guarded reader's probe error", err)
	}
	if _, err := admission.ListSecretsIAMPostureGaps(ctx, IAMPostureGapFilter{ScopeID: "scope-1", Limit: 10}); !errors.Is(err, errSecretsIAMWireShapeProbe) {
		t.Errorf("ListSecretsIAMPostureGaps over an unavailable reader = %v, want the guarded reader's probe error", err)
	}
	if _, err := admission.SummarizeSecretsIAMPosture(ctx, "scope-1"); !errors.Is(err, errSecretsIAMWireShapeProbe) {
		t.Errorf("SummarizeSecretsIAMPosture over an unavailable reader = %v, want the guarded reader's probe error", err)
	}
}

// TestSecretsIAMReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the five re-declared secrets/IAM reads, with no
// S3 external-principal grant surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestSecretsIAMReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*SecretsIAMReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 5 {
		t.Fatalf("SecretsIAMReadAdmission has %d methods, want exactly 5: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"Grant", "S3", "ExternalPrincipal", "Snapshot", "Ledger"} {
			if strings.Contains(name, banned) {
				t.Errorf("SecretsIAMReadAdmission.%s contains %q: the grant, snapshot, and ledger legs stay out of this pin", name, banned)
			}
		}
	}
}
