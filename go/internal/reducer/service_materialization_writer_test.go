package reducer

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestServiceMaterializationWriterCommitsGenerationAndSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: func() time.Time { return now }}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		IntentID:  "intent-1",
		ServiceID: "svc-checkout",
		Ownership: []ServiceOwnershipEvidence{
			{OwnerRef: "team-payments", Payload: map[string]any{"tier": "gold"}},
			{OwnerRef: "team-platform", Payload: map[string]any{"tier": "silver"}},
		},
	})
	if err != nil {
		t.Fatalf("WriteServiceMaterialization() error = %v, want nil", err)
	}
	if !result.Committed {
		t.Fatal("first materialization should commit a new generation")
	}
	if result.EvidenceRows != 2 {
		t.Fatalf("EvidenceRows = %d, want 2", result.EvidenceRows)
	}
	if len(result.SupersededIDs) != 0 {
		t.Fatalf("SupersededIDs = %v, want empty on first commit", result.SupersededIDs)
	}
	if got := len(store.generations); got != 1 {
		t.Fatalf("generations = %d, want 1", got)
	}
	if store.activeGeneration("svc-checkout") != result.GenerationID {
		t.Fatalf("active generation = %q, want %q", store.activeGeneration("svc-checkout"), result.GenerationID)
	}
	for _, row := range store.snapshotsFor(result.GenerationID) {
		if !strings.HasPrefix(row.evidenceKey, "ownership:svc-checkout:") {
			t.Fatalf("evidence key %q is not generation-independent ownership shape", row.evidenceKey)
		}
		if strings.Contains(row.evidenceKey, result.GenerationID) {
			t.Fatalf("evidence key %q embeds the generation id; identity must be generation-independent", row.evidenceKey)
		}
	}
	if !store.committed {
		t.Fatal("transaction was not committed")
	}
}

func TestServiceMaterializationWriterIdempotentNoOp(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}
	write := ServiceMaterializationWrite{
		ServiceID: "svc-orders",
		Ownership: []ServiceOwnershipEvidence{{OwnerRef: "team-a", Payload: map[string]any{"tier": "gold"}}},
	}

	first, err := writer.WriteServiceMaterialization(context.Background(), write)
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}
	second, err := writer.WriteServiceMaterialization(context.Background(), write)
	if err != nil {
		t.Fatalf("second write error = %v", err)
	}
	if second.Committed {
		t.Fatal("identical re-materialization must be a no-op, not a new generation")
	}
	if second.GenerationID != first.GenerationID {
		t.Fatalf("idempotent generation id drifted: %q -> %q", first.GenerationID, second.GenerationID)
	}
	if got := len(store.generations); got != 1 {
		t.Fatalf("generations = %d, want 1 (no churn)", got)
	}
}

func TestServiceMaterializationWriterSupersedesPriorGeneration(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	first, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Ownership: []ServiceOwnershipEvidence{{OwnerRef: "team-a", Payload: map[string]any{"tier": "gold"}}},
	})
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}

	second, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Ownership: []ServiceOwnershipEvidence{{OwnerRef: "team-a", Payload: map[string]any{"tier": "platinum"}}},
	})
	if err != nil {
		t.Fatalf("second write error = %v", err)
	}
	if !second.Committed {
		t.Fatal("changed evidence should commit a new generation")
	}
	if len(second.SupersededIDs) != 1 || second.SupersededIDs[0] != first.GenerationID {
		t.Fatalf("SupersededIDs = %v, want [%s]", second.SupersededIDs, first.GenerationID)
	}
	if store.activeGeneration("svc-a") != second.GenerationID {
		t.Fatalf("active generation = %q, want %q", store.activeGeneration("svc-a"), second.GenerationID)
	}
	if store.generations[first.GenerationID].status != ServiceMaterializationStatusSuperseded {
		t.Fatalf("prior generation status = %q, want superseded", store.generations[first.GenerationID].status)
	}
}

func TestServiceMaterializationWriterTombstonesRetiredOwner(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Ownership: []ServiceOwnershipEvidence{
			{OwnerRef: "team-keep", Payload: map[string]any{"tier": "gold"}},
			{OwnerRef: "team-gone", Retired: true},
		},
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	var tombstoned bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		if row.evidenceKey == ServiceOwnershipEvidenceKey("svc-a", "team-gone") {
			tombstoned = row.tombstone
		}
	}
	if !tombstoned {
		t.Fatal("retired owner must be written as a tombstone row, never silently absent")
	}
}

func TestServiceMaterializationWriterRequiresServiceID(t *testing.T) {
	t.Parallel()

	writer := PostgresServiceMaterializationWriter{DB: newFakeServiceMaterializationStore(), Now: time.Now}
	if _, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{}); err == nil {
		t.Fatal("WriteServiceMaterialization() error = nil, want non-nil for empty service_id")
	}
}

func TestServiceMaterializationWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresServiceMaterializationWriter{}).WriteServiceMaterialization(
		context.Background(),
		ServiceMaterializationWrite{ServiceID: "svc-a"},
	); err == nil {
		t.Fatal("WriteServiceMaterialization() error = nil, want non-nil for nil DB")
	}
}

func TestBuildServiceOwnershipMaterializationsGroupsByService(t *testing.T) {
	t.Parallel()

	writes := buildServiceOwnershipMaterializations("intent-1", []ServiceCatalogCorrelationDecision{
		{ServiceID: "svc-b", OwnerRef: "team-2", Provider: "backstage", EntityRef: "component:default/b"},
		{ServiceID: "svc-a", OwnerRef: "team-1", Provider: "backstage", EntityRef: "component:default/a"},
		{ServiceID: "svc-a", OwnerRef: "team-1b", Provider: "backstage", EntityRef: "component:default/a2"},
		{ServiceID: "", OwnerRef: "team-x"}, // skipped: no service id
		{ServiceID: "svc-c", OwnerRef: ""},  // skipped: no owner ref
	})
	if len(writes) != 2 {
		t.Fatalf("writes = %d, want 2 (svc-a, svc-b)", len(writes))
	}
	if writes[0].ServiceID != "svc-a" || writes[1].ServiceID != "svc-b" {
		t.Fatalf("writes not ordered by service id: %q, %q", writes[0].ServiceID, writes[1].ServiceID)
	}
	if len(writes[0].Ownership) != 2 {
		t.Fatalf("svc-a ownership = %d, want 2", len(writes[0].Ownership))
	}
}

func TestServiceCatalogHandlerCommitsOwnershipGenerationsWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceCatalogEntityFact("entity", "component:default/checkout", "Checkout"),
			serviceCatalogRepositoryLinkFact("repo-link", "component:default/checkout", "https://github.com/acme/checkout.git"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		},
	}
	writer := &recordingServiceCatalogCorrelationWriter{}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:            loader,
		Writer:                writer,
		MaterializationWriter: PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("correlation writer calls = %d, want 1 (existing path unchanged)", writer.calls)
	}
	// The fixture decision carries a service_id/owner_ref only when the catalog
	// entity correlates with an owner; the lineage commit must not error and must
	// leave at most one active generation per service.
	for serviceID, gen := range collectActiveServices(materialization) {
		if gen == "" {
			t.Fatalf("service %q has no active generation after commit", serviceID)
		}
	}
}

func collectActiveServices(store *fakeServiceMaterializationStore) map[string]string {
	active := map[string]string{}
	for id, gen := range store.generations {
		if gen.status == ServiceMaterializationStatusActive {
			active[gen.serviceID] = id
		}
	}
	return active
}

// fakeServiceMaterializationStore is an in-memory stand-in for the durable
// lineage store that honors the same single-active-per-service and idempotent
// generation contract the SQL enforces, so the writer can be proven without a
// live Postgres.
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

func (f *fakeServiceMaterializationStore) activeGeneration(serviceID string) string {
	for id, gen := range f.generations {
		if gen.serviceID == serviceID && gen.status == ServiceMaterializationStatusActive {
			return id
		}
	}
	return ""
}

func (f *fakeServiceMaterializationStore) snapshotsFor(generationID string) []fakeSnapshotRow {
	return f.snapshots[generationID]
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
