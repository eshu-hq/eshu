// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// EvidenceStore persists relationship evidence facts for a generation. It is
// the seam through which the deferred-backfill engine reaches the relationship
// evidence table without importing the parent postgres package (which would be
// an import cycle): the root IngestionStore supplies a constructor that wraps
// its own relationship store, and every engine write goes through this
// interface. *postgres.RelationshipStore satisfies it.
type EvidenceStore interface {
	UpsertEvidenceFacts(ctx context.Context, generationID string, evidence []relationships.EvidenceFact) error
	UpsertEvidenceFactsCounted(ctx context.Context, generationID string, evidence []relationships.EvidenceFact) (int64, error)
}

// PhaseStore publishes graph-projection phase rows. It is the seam through
// which the fan-in publication reaches the phase table without importing the
// parent postgres package: the root seam supplies a constructor that wraps its
// own graph-projection phase store. *postgres.GraphProjectionPhaseStateStore
// satisfies it.
type PhaseStore interface {
	PublishGraphProjectionPhases(ctx context.Context, rows []reducer.GraphProjectionPhaseState) error
}

// CatalogLoader loads the deduplicated repository identity catalog plus each
// kept entry's observed_at. It is the seam through which the engine reaches
// the root catalog loader without importing the parent postgres package: the
// root seam supplies loadRepositoryCatalog. The observed-at map is the shared
// cache's freshness key; engine passes that only read the entries may ignore
// it.
type CatalogLoader func(ctx context.Context, queryer db.Queryer) ([]relationships.CatalogEntry, map[string]time.Time, error)

// Store carries the durable handles and operator configuration the
// deferred-backfill engine needs. It mirrors the IngestionStore fields the
// engine used before the extraction (issue #7794): the database handle, the
// transaction beginner, the per-batch repository count and worker count
// overrides, the clock, the logger, and the instruments. The zero value is
// usable for pure reads; a nil Beginner fails batch writes the same way a
// store built without one always did.
//
// The root IngestionStore builds a Store through its backfill seam, supplying
// the catalog loader and the evidence-store constructor beside the shared
// handles, so the engine never imports the parent package. Tests build a
// Store directly over fake handles.
type Store struct {
	DB               db.ExecQueryer
	Beginner         db.Beginner
	BatchSize        int
	Workers          int
	Now              func() time.Time
	Logger           *slog.Logger
	Instruments      *telemetry.Instruments
	LoadCatalog      CatalogLoader
	NewEvidenceStore func(tx db.Transaction) EvidenceStore
	NewPhaseStore    func(tx db.Transaction) PhaseStore
}

// NewStore builds a Store over database, adopting the transaction beginner
// when the handle offers one, exactly like NewIngestionStore. Callers that
// need catalog loads or evidence writes set LoadCatalog, NewEvidenceStore,
// and NewPhaseStore. A nil LoadCatalog is not an error: the pass runs with
// an empty catalog, so the anchor-scoped load finds nothing and the pass is
// vacuous. A nil evidence or phase constructor fails the write paths fast,
// the same as a store built without a database.
func NewStore(database db.ExecQueryer) Store {
	store := Store{DB: database}
	if beginner, ok := database.(db.Beginner); ok {
		store.Beginner = beginner
	}
	return store
}

// now reports the store clock, defaulting to the wall clock when unset.
func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
