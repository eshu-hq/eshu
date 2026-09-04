// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// This file is a local copy of test fixtures and doubles the
// service-catalog-correlation/service-materialization family's own test suite
// defined before it moved to internal/reducer/servicecatalog (issue #6061). Go
// test files cannot share unexported symbols across a package boundary, and
// the still-in-root defaults_cicd_test.go and service_runtime_instance_lookup_test.go
// exercise root's own wiring (implementedDefaultDomainDefinitions,
// GraphServiceRuntimeInstanceLoader) against the family's exported entry
// points (ServiceCatalogCorrelationHandler, PostgresServiceMaterializationWriter),
// so those tests stay in root as cross-family-contract pins and need this
// trimmed local copy rather than requiring every root test to import
// servicecatalog and requalify every call site. Mirrors
// internal/reducer/containerimage's own
// container_image_identity_root_test_doubles_test.go for the same reason.

// stubServiceCatalogCorrelationFactLoader is a local copy of
// internal/reducer/servicecatalog's own fixture, trimmed to the surface these
// root tests exercise.
type stubServiceCatalogCorrelationFactLoader struct {
	scopeFacts      []facts.Envelope
	activeRepos     []facts.Envelope
	kindCalls       [][]string
	repositoryCalls int
}

func (s *stubServiceCatalogCorrelationFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubServiceCatalogCorrelationFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubServiceCatalogCorrelationFactLoader) ListActiveRepositoryFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.repositoryCalls++
	return append([]facts.Envelope(nil), s.activeRepos...), nil
}

// recordingServiceCatalogCorrelationWriter is a local copy of
// internal/reducer/servicecatalog's own fixture.
type recordingServiceCatalogCorrelationWriter struct {
	write ServiceCatalogCorrelationWrite
	calls int
}

func (w *recordingServiceCatalogCorrelationWriter) WriteServiceCatalogCorrelations(
	_ context.Context,
	write ServiceCatalogCorrelationWrite,
) (ServiceCatalogCorrelationWriteResult, error) {
	w.calls++
	w.write = write
	return ServiceCatalogCorrelationWriteResult{
		FactsWritten: len(write.Decisions),
	}, nil
}

// fakeServiceScopedIncidentLoader is a local copy of
// internal/reducer/servicecatalog's own fixture.
type fakeServiceScopedIncidentLoader struct {
	byService map[string][]ServiceIncidentRecord
	calls     int
}

func (f *fakeServiceScopedIncidentLoader) GetIncidentEvidenceForServices(
	_ context.Context,
	serviceIDs []string,
) (map[string][]ServiceIncidentRecord, error) {
	f.calls++
	out := map[string][]ServiceIncidentRecord{}
	for _, serviceID := range serviceIDs {
		out[serviceID] = f.byService[serviceID]
	}
	return out, nil
}

// serviceCatalogEntityFact, serviceCatalogOwnershipFact,
// serviceCatalogRepositoryIDLinkFact, serviceTypedCatalogEntityFact, and
// repositoryFact are local copies of internal/reducer/servicecatalog's own
// fixture builders, trimmed to the surface these root tests exercise.

func serviceCatalogEntityFact(factID, entityRef, displayName string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.ServiceCatalogEntityFactKind,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":     "backstage",
			"entity_ref":   entityRef,
			"entity_type":  "component",
			"display_name": displayName,
			"lifecycle":    "production",
			"tier":         "tier_1",
		},
	}
}

// serviceTypedCatalogEntityFact marks the entity type "service" and stamps a
// service_id, so the correlation decision admits a non-empty ServiceID and
// the per-service materialization runs.
func serviceTypedCatalogEntityFact(factID, entityRef, displayName string) facts.Envelope {
	envelope := serviceCatalogEntityFact(factID, entityRef, displayName)
	envelope.Payload["entity_type"] = "service"
	envelope.Payload["service_id"] = "svc-checkout"
	return envelope
}

func serviceCatalogOwnershipFact(factID, entityRef, ownerRef string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.ServiceCatalogOwnershipFactKind,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":   "backstage",
			"entity_ref": entityRef,
			"owner_ref":  ownerRef,
		},
	}
}

func serviceCatalogRepositoryIDLinkFact(factID, entityRef, repositoryID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.ServiceCatalogRepositoryLinkFactKind,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":      "backstage",
			"entity_ref":    entityRef,
			"repository_id": repositoryID,
		},
	}
}

func repositoryFact(factID, name, remoteURL string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID:      factID,
		FactKind:    factload.FactKindRepository,
		IsTombstone: tombstone,
		Payload: map[string]any{
			"repo_id":    factID,
			"name":       name,
			"remote_url": remoteURL,
		},
	}
}

// fakeServiceMaterializationStore, its supporting types, and
// newFakeServiceMaterializationStore are a local copy of
// internal/reducer/servicecatalog's own fixture: an in-memory
// ServiceMaterializationBeginner that enforces the same single-active-per-
// generation contract the SQL enforces, so root's wiring tests can exercise
// ServiceCatalogCorrelationHandler.Handle end to end without a live Postgres.
type fakeServiceMaterializationStore struct {
	generations map[string]*fakeServiceGeneration
	snapshots   map[string][]fakeSnapshotRow
	committed   bool
	rolledBack  bool
}

type fakeServiceGeneration struct {
	serviceID string
	status    string
}

type fakeSnapshotRow struct {
	evidenceKey string
	tombstone   bool
}

func newFakeServiceMaterializationStore() *fakeServiceMaterializationStore {
	return &fakeServiceMaterializationStore{
		generations: map[string]*fakeServiceGeneration{},
		snapshots:   map[string][]fakeSnapshotRow{},
	}
}

func (f *fakeServiceMaterializationStore) BeginServiceMaterializationTx(
	context.Context,
) (ServiceMaterializationTx, error) {
	return &fakeServiceMaterializationTx{store: f}, nil
}

type fakeServiceMaterializationTx struct {
	store *fakeServiceMaterializationStore
}

func (t *fakeServiceMaterializationTx) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO service_materialization_generations"):
		generationID := args[0].(string)
		serviceID := args[1].(string)
		if _, exists := t.store.generations[generationID]; exists {
			return fakeServiceMaterializationResult{affected: 0}, nil
		}
		// New generations are inserted pending, then promoted by the activate
		// UPDATE, mirroring the single-active-per-service ordering the SQL enforces.
		t.store.generations[generationID] = &fakeServiceGeneration{
			serviceID: serviceID,
			status:    ServiceMaterializationStatusPending,
		}
		return fakeServiceMaterializationResult{affected: 1}, nil
	case strings.Contains(query, "SET status = 'active'"):
		generationID := args[1].(string)
		if gen, ok := t.store.generations[generationID]; ok && gen.status == ServiceMaterializationStatusPending {
			gen.status = ServiceMaterializationStatusActive
			return fakeServiceMaterializationResult{affected: 1}, nil
		}
		return fakeServiceMaterializationResult{affected: 0}, nil
	case strings.Contains(query, "INSERT INTO service_evidence_snapshots"):
		generationID := args[0].(string)
		t.store.snapshots[generationID] = append(t.store.snapshots[generationID], fakeSnapshotRow{
			evidenceKey: args[3].(string),
			tombstone:   args[5].(bool),
		})
		return fakeServiceMaterializationResult{affected: 1}, nil
	default:
		return fakeServiceMaterializationResult{}, nil
	}
}

func (t *fakeServiceMaterializationTx) QueryRowContext(
	_ context.Context,
	_ string,
	args ...any,
) ServiceMaterializationRow {
	serviceID := args[0].(string)
	newGeneration := args[1].(string)
	for id, gen := range t.store.generations {
		if gen.serviceID == serviceID && gen.status == ServiceMaterializationStatusActive && id != newGeneration {
			gen.status = ServiceMaterializationStatusSuperseded
			return fakeServiceMaterializationRow{value: id}
		}
	}
	return fakeServiceMaterializationRow{noRows: true}
}

func (t *fakeServiceMaterializationTx) Commit() error {
	t.store.committed = true
	return nil
}

func (t *fakeServiceMaterializationTx) Rollback() error {
	t.store.rolledBack = true
	return nil
}

type fakeServiceMaterializationResult struct {
	affected int64
}

func (r fakeServiceMaterializationResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeServiceMaterializationResult) RowsAffected() (int64, error) { return r.affected, nil }

type fakeServiceMaterializationRow struct {
	value  string
	noRows bool
}

func (r fakeServiceMaterializationRow) Scan(dest ...any) error {
	if r.noRows {
		return sql.ErrNoRows
	}
	*dest[0].(*sql.NullString) = sql.NullString{String: r.value, Valid: true}
	return nil
}
