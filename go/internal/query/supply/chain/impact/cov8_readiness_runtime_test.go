// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// cov8ImpactRows is an in-memory db.Rows double: Next walks the canned
// value rows, Scan assigns them positionally, Err injects a failure.
type cov8ImpactRows struct {
	rows    [][]any
	pos     int
	scanErr error
	rowErr  error
	closes  int
}

func (r *cov8ImpactRows) Next() bool {
	if r.pos < len(r.rows) {
		r.pos++
		return true
	}
	return false
}

func (r *cov8ImpactRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	values := r.rows[r.pos-1]
	if len(values) != len(dest) {
		return fmt.Errorf("cov8: row has %d values for %d destinations", len(values), len(dest))
	}
	for i, d := range dest {
		if err := cov8ImpactAssign(d, values[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *cov8ImpactRows) Err() error { return r.rowErr }

func (r *cov8ImpactRows) Close() error {
	r.closes++
	return nil
}

// cov8ImpactAssign mimics database/sql conversion for the readiness and
// runtime-context scan shapes, including sql.Scanner destinations.
func cov8ImpactAssign(dest, value any) error {
	if scanner, ok := dest.(sql.Scanner); ok {
		return scanner.Scan(value)
	}
	switch d := dest.(type) {
	case *string:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("cov8: cannot scan %T into *string", value)
		}
		*d = s
		return nil
	case *[]byte:
		switch v := value.(type) {
		case []byte:
			*d = append([]byte(nil), v...)
			return nil
		case string:
			*d = []byte(v)
			return nil
		default:
			return fmt.Errorf("cov8: cannot scan %T into *[]byte", value)
		}
	case *int:
		n, ok := value.(int)
		if !ok {
			return fmt.Errorf("cov8: cannot scan %T into *int", value)
		}
		*d = n
		return nil
	case *bool:
		b, ok := value.(bool)
		if !ok {
			return fmt.Errorf("cov8: cannot scan %T into *bool", value)
		}
		*d = b
		return nil
	default:
		return fmt.Errorf("cov8: unsupported destination %T", dest)
	}
}

// cov8ImpactTx serves one canned response per QueryContext call in order.
type cov8ImpactTx struct {
	steps     []cov8ImpactStep
	queries   int
	commits   int
	rollbacks int
	commitErr error
}

type cov8ImpactStep struct {
	rows    [][]any
	rowsErr error
	err     error
}

func (tx *cov8ImpactTx) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	tx.queries++
	if len(tx.steps) == 0 {
		return &cov8ImpactRows{}, nil
	}
	step := tx.steps[0]
	tx.steps = tx.steps[1:]
	if step.err != nil {
		return nil, step.err
	}
	return &cov8ImpactRows{rows: step.rows, rowErr: step.rowsErr}, nil
}

func (tx *cov8ImpactTx) QueryRowContext(context.Context, string, ...any) db.Row {
	return nil
}

func (tx *cov8ImpactTx) Commit() error {
	tx.commits++
	return tx.commitErr
}

func (tx *cov8ImpactTx) Rollback() error {
	tx.rollbacks++
	return nil
}

// cov8AuthorityPresentStep scripts the authority-layout probe every main
// read issues before its business query. The layout reads present, so the
// protected statement runs exactly as on a fully bootstrapped database.
func cov8AuthorityPresentStep() cov8ImpactStep {
	return cov8ImpactStep{rows: [][]any{{true}}}
}

// cov8AuthorityAbsentStep scripts the probe for a schema without the
// authority objects: the legacy statement serves the business query.
func cov8AuthorityAbsentStep() cov8ImpactStep {
	return cov8ImpactStep{rows: [][]any{{false}}}
}

// cov8ImpactReadStore opens the scripted transaction.
type cov8ImpactReadStore struct {
	tx       *cov8ImpactTx
	beginErr error
}

func (s *cov8ImpactReadStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("cov8: readiness must query within the snapshot")
}

func (s *cov8ImpactReadStore) QueryRowContext(context.Context, string, ...any) db.Row {
	return nil
}

func (s *cov8ImpactReadStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	if s.beginErr != nil {
		return nil, s.beginErr
	}
	return s.tx, nil
}

// cov8ReadinessRow renders one readiness scan row.
func cov8ReadinessRow(family string, count int, latest any, incomplete any, reasons any, snapshots, states, targets any) []any {
	return []any{family, count, latest, incomplete, reasons, snapshots, states, targets}
}

// TestCov8ReadinessStoreGuards pins the entry gates: no database fails
// loud, and a query without a fact anchor returns empty without SQL.
func TestCov8ReadinessStoreGuards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if _, err := (PostgresReadinessStore{}).ReadSupplyChainImpactReadiness(
		ctx, ReadinessQuery{RepositoryID: "repo-1"}); err == nil {
		t.Error("storeless anchored read = nil, want database-required error")
	}
	store := NewPostgresReadinessStore(nil)
	if store.FreshnessWindow != supplyChainImpactReadinessFreshnessWindow {
		t.Errorf("default window = %v, want 14 days", store.FreshnessWindow)
	}
	// The store guard runs before the anchor check: even an anchorless
	// query needs a configured store.
	if _, err := store.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{Severity: "critical"}); err == nil {
		t.Error("storeless anchorless read = nil, want database-required error")
	}
	// With a store but no anchor, the read stays empty without SQL.
	tx := &cov8ImpactTx{}
	anchored := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{tx: tx})
	snapshot, err := anchored.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{Severity: "critical"})
	if err != nil {
		t.Fatalf("anchorless read = %v, want nil", err)
	}
	if len(snapshot.EvidenceSources) != 0 || snapshot.TargetIncomplete {
		t.Errorf("anchorless snapshot = %+v, want empty", snapshot)
	}
	if tx.queries != 0 {
		t.Errorf("anchorless queries = %d, want no SQL", tx.queries)
	}
	if store.FreshnessWindow != supplyChainImpactReadinessFreshnessWindow {
		t.Errorf("default window = %v, want 14 days", store.FreshnessWindow)
	}
}

// TestCov8ReadinessSnapshotRows pins the snapshot fold: fresh, stale,
// and unknown families aggregate by count, marker rows decode metadata,
// and incomplete markers fail the target closed.
func TestCov8ReadinessSnapshotRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fresh := time.Now().UTC().Add(-time.Hour)
	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)
	tx := &cov8ImpactTx{steps: []cov8ImpactStep{cov8AuthorityPresentStep(), {rows: [][]any{
		cov8ReadinessRow(EvidenceFamilyVulnerabilityAdvisory, 2, fresh, false, nil, nil, nil, nil),
		cov8ReadinessRow(EvidenceFamilyVulnerabilityAdvisory, 1, stale, nil, nil, nil, nil, nil),
		cov8ReadinessRow(EvidenceFamilyPackageRegistry, 1, nil, nil, nil, nil, nil, nil),
		// Zero counts and unknown families never surface.
		cov8ReadinessRow(EvidenceFamilySBOMComponent, 0, fresh, nil, nil, nil, nil, nil),
		cov8ReadinessRow("not.a.family", 9, fresh, nil, nil, nil, nil, nil),
		// Marker rows fold metadata instead of counts.
		cov8ReadinessRow(sourceSnapshotFamilyMarker, 0, nil, true, "{stale-cache}", `[{"source":"nvd"}]`, nil, nil),
		cov8ReadinessRow(sourceStateFamilyMarker, 0, nil, false, nil, nil, `[]`, nil),
		cov8ReadinessRow(unsupportedTargetFamilyMarker, 0, nil, nil, nil, nil, nil, `[]`),
	}}}}
	store := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{tx: tx})
	snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{RepositoryID: "repo-1"})
	if err != nil {
		t.Fatalf("snapshot read = %v, want nil", err)
	}
	byFamily := map[string]EvidenceFamily{}
	for _, family := range snapshot.EvidenceSources {
		byFamily[family.Family] = family
	}
	advisory, ok := byFamily[EvidenceFamilyVulnerabilityAdvisory]
	if !ok || advisory.FactCount != 3 {
		t.Errorf("advisory family = %+v, want aggregated count 3", advisory)
	}
	if advisory.Freshness != FreshnessLabelFresh {
		t.Errorf("advisory freshness = %q, want fresh (latest observation wins)", advisory.Freshness)
	}
	registry, ok := byFamily[EvidenceFamilyPackageRegistry]
	if !ok || registry.Freshness != FreshnessLabelUnknown {
		t.Errorf("registry family = %+v, want unknown freshness", registry)
	}
	if _, ok := byFamily[EvidenceFamilySBOMComponent]; ok {
		t.Error("zero-count family surfaces, want skipped")
	}
	if _, ok := byFamily["not.a.family"]; ok {
		t.Error("unknown family surfaces, want skipped")
	}
	if !snapshot.TargetIncomplete {
		t.Error("TargetIncomplete = false, want true from the snapshot marker")
	}
	if len(snapshot.IncompleteReasons) != 1 || snapshot.IncompleteReasons[0] != "stale-cache" {
		t.Errorf("incomplete reasons = %v, want [stale-cache]", snapshot.IncompleteReasons)
	}
	if len(snapshot.SourceSnapshots) != 1 || snapshot.SourceSnapshots[0].Source != "nvd" {
		t.Errorf("source snapshots = %+v, want the nvd entry", snapshot.SourceSnapshots)
	}
	if tx.commits != 1 || tx.rollbacks != 1 || tx.queries != 2 {
		t.Errorf("tx use = %d queries %d commits %d rollbacks, want 2 1 1 (authority probe plus business query)",
			tx.queries, tx.commits, tx.rollbacks)
	}
}

// TestCov8ReadinessStaleFamily pins the stale arm: an observation older
// than the window labels the family stale rather than fresh.
func TestCov8ReadinessStaleFamily(t *testing.T) {
	t.Parallel()

	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)
	tx := &cov8ImpactTx{steps: []cov8ImpactStep{cov8AuthorityPresentStep(), {rows: [][]any{
		cov8ReadinessRow(EvidenceFamilyScannerWorkerAnalysis, 1, stale, nil, nil, nil, nil, nil),
	}}}}
	store := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{tx: tx})
	snapshot, err := store.ReadSupplyChainImpactReadiness(
		context.Background(), ReadinessQuery{RepositoryID: "repo-1"})
	if err != nil {
		t.Fatalf("stale read = %v, want nil", err)
	}
	if len(snapshot.EvidenceSources) != 1 ||
		snapshot.EvidenceSources[0].Freshness != FreshnessLabelStale {
		t.Errorf("sources = %+v, want one stale family", snapshot.EvidenceSources)
	}
}

// TestCov8ReadinessErrorArms pins failure propagation through the
// snapshot: query, scan, cursor, marker-decode, and commit failures.
func TestCov8ReadinessErrorArms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	query := ReadinessQuery{RepositoryID: "repo-1"}

	want := errors.New("snapshot query down")
	store := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{
		tx: &cov8ImpactTx{steps: []cov8ImpactStep{cov8AuthorityPresentStep(), {err: want}}},
	})
	if _, err := store.ReadSupplyChainImpactReadiness(ctx, query); !errors.Is(err, want) {
		t.Errorf("query failure = %v, want wrap of %v", err, want)
	}

	badScan := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{
		tx: &cov8ImpactTx{steps: []cov8ImpactStep{cov8AuthorityPresentStep(), {rows: [][]any{{42}}}}},
	})
	if _, err := badScan.ReadSupplyChainImpactReadiness(ctx, query); err == nil {
		t.Error("scan failure = nil, want wrapped scan error")
	}

	cursorDown := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{
		tx: &cov8ImpactTx{steps: []cov8ImpactStep{cov8AuthorityPresentStep(), {
			rows:    [][]any{cov8ReadinessRow(EvidenceFamilyPackageRegistry, 1, nil, nil, nil, nil, nil, nil)},
			rowsErr: errors.New("cursor lost"),
		}}},
	})
	if _, err := cursorDown.ReadSupplyChainImpactReadiness(ctx, query); err == nil {
		t.Error("cursor failure = nil, want wrapped rows error")
	}

	badMarker := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{
		tx: &cov8ImpactTx{steps: []cov8ImpactStep{cov8AuthorityPresentStep(), {rows: [][]any{
			cov8ReadinessRow(sourceSnapshotFamilyMarker, 0, nil, false, nil, "{oops", nil, nil),
		}}}},
	})
	if _, err := badMarker.ReadSupplyChainImpactReadiness(ctx, query); err == nil {
		t.Error("marker decode failure = nil, want wrapped decode error")
	}

	commitDown := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{
		tx: &cov8ImpactTx{
			steps:     []cov8ImpactStep{cov8AuthorityPresentStep()},
			commitErr: errors.New("commit down"),
		},
	})
	if _, err := commitDown.ReadSupplyChainImpactReadiness(ctx, query); err == nil {
		t.Error("commit failure = nil, want wrapped commit error")
	}
}

// TestCov8ReadinessTargetResolutionGate pins the ordering gate: a CVE
// query with an unready manifest backfill fails closed, and a query
// error surfaces, before any target SQL runs.
func TestCov8ReadinessTargetResolutionGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	notReady := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{
		tx: &cov8ImpactTx{steps: []cov8ImpactStep{{rows: [][]any{{false}}}}},
	})
	if _, err := notReady.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{CVEID: "CVE-2026-1"}); err == nil {
		t.Error("unready manifest read = nil, want not-ready error")
	}

	ready, err := packageManifestConsumptionKeysReady(ctx, &cov8ImpactTx{
		steps: []cov8ImpactStep{{rows: [][]any{{true}}}},
	})
	if err != nil || !ready {
		t.Errorf("ready check = %v, %v; want true, nil", ready, err)
	}
	want := errors.New("ready check down")
	if _, err := packageManifestConsumptionKeysReady(ctx, &cov8ImpactTx{
		steps: []cov8ImpactStep{{err: want}},
	}); !errors.Is(err, want) {
		t.Errorf("ready query failure = %v, want wrap of %v", err, want)
	}
}

// cov8NarrowReadinessDB implements only ReadinessQueryer: it cannot
// start the repeatable-read transaction target resolution needs.
type cov8NarrowReadinessDB struct{}

func (cov8NarrowReadinessDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("cov8: unreachable without a transaction")
}

// TestCov8ReadinessLegacyTargetNeedsTransaction pins the legacy guard: a
// CVE query against a transaction-less queryer fails before SQL.
func TestCov8ReadinessLegacyTargetNeedsTransaction(t *testing.T) {
	t.Parallel()

	store := NewPostgresReadinessStore(cov8NarrowReadinessDB{})
	_, err := store.ReadSupplyChainImpactReadiness(
		context.Background(), ReadinessQuery{CVEID: "CVE-2026-1"})
	if err == nil {
		t.Error("legacy CVE read without transaction = nil, want requires-transaction error")
	}
}

// TestCov8RecordRuntimeEnvironmentEvidence pins the deploy-event fold:
// only deploy_event proves corroboration, missing values declare, and a
// recorded deploy_event sticks independent of iteration order.
func TestCov8RecordRuntimeEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	if got := RecordRuntimeEnvironmentEvidence(nil, "  ", "deploy_event"); got != nil {
		t.Errorf("blank environment = %v, want untouched nil", got)
	}
	state := RecordRuntimeEnvironmentEvidence(nil, "prod", "deploy_event")
	if state["prod"] != RuntimeEnvironmentEvidenceDeployEvent {
		t.Errorf("deploy_event records %v, want deploy_event", state)
	}
	// A sticky deploy_event survives a later declared observation.
	state = RecordRuntimeEnvironmentEvidence(state, "prod", "declared")
	if state["prod"] != RuntimeEnvironmentEvidenceDeployEvent {
		t.Errorf("sticky deploy_event = %v, want deploy_event kept", state)
	}
	state = RecordRuntimeEnvironmentEvidence(state, "staging", "whatever")
	if state["staging"] != RuntimeEnvironmentEvidenceDeclared {
		t.Errorf("unknown value records %v, want declared", state)
	}
	if state["prod"] != RuntimeEnvironmentEvidenceDeployEvent {
		t.Errorf("prod after staging fold = %v, want deploy_event kept", state)
	}
}

// TestCov8ListRuntimeContext pins the read-time join: empty ids stay
// empty, a missing store fails loud, and rows fold per repository with
// outcome gating.
func TestCov8ListRuntimeContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := NewPostgresFindingStoreWithReadStore(&cov8ImpactTx{}, false)
	out, err := store.ListSupplyChainImpactRuntimeContext(ctx, nil, nil, nil)
	if err != nil || len(out) != 0 {
		t.Errorf("empty ids = %v, %v; want empty, nil", out, err)
	}
	if _, err := (PostgresFindingStore{}).ListSupplyChainImpactRuntimeContext(
		ctx, []string{"repo-1"}, nil, nil); err == nil {
		t.Error("storeless runtime context = nil, want database-required error")
	}
	want := errors.New("context query down")
	down := NewPostgresFindingStoreWithReadStore(&cov8ImpactTx{
		steps: []cov8ImpactStep{{err: want}},
	}, false)
	if _, err := down.ListSupplyChainImpactRuntimeContext(ctx, []string{"repo-1"}, nil, nil); !errors.Is(err, want) {
		t.Errorf("query failure = %v, want wrap of %v", err, want)
	}

	workloadPayload, _ := json.Marshal(map[string]any{"workload_id": "workload:web"})
	rejectedPayload, _ := json.Marshal(map[string]any{
		"service_id": "svc-x", "outcome": "rejected",
	})
	envPayload, _ := json.Marshal(map[string]any{"environment": "prod", "outcome": "exact"})
	rows := NewPostgresFindingStoreWithReadStore(&cov8ImpactTx{steps: []cov8ImpactStep{{rows: [][]any{
		{WorkloadIdentityFactKindQuery, "scope-1", workloadPayload, "repo-1"},
		{"reducer_service_catalog_correlation", "scope-1", rejectedPayload, "repo-1"},
		{"reducer_ci_cd_run_correlation", "scope-1", envPayload, "repo-1"},
		{"reducer_platform_materialization", "scope-1", []byte(`{"deployment_id":"dep-1"}`), "repo-2"},
	}}}}, false)
	got, err := rows.ListSupplyChainImpactRuntimeContext(ctx, []string{"repo-1", "repo-2"}, nil, nil)
	if err != nil {
		t.Fatalf("runtime context = %v, want nil", err)
	}
	repo1, ok := got["repo-1"]
	if !ok || len(repo1.WorkloadIDs) != 1 || len(repo1.Environments) != 1 {
		t.Errorf("repo-1 context = %+v, want one workload plus one environment", repo1)
	}
	if len(repo1.ServiceIDs) != 0 {
		t.Errorf("rejected service catalog row folds %v, want skipped", repo1.ServiceIDs)
	}
	if repo2, ok := got["repo-2"]; !ok || len(repo2.DeploymentIDs) != 1 {
		t.Errorf("repo-2 context = %+v, want one deployment", repo2)
	}

	badJSON := NewPostgresFindingStoreWithReadStore(&cov8ImpactTx{steps: []cov8ImpactStep{{rows: [][]any{
		{WorkloadIdentityFactKindQuery, "scope-1", []byte("{oops"), "repo-1"},
	}}}}, false)
	if _, err := badJSON.ListSupplyChainImpactRuntimeContext(ctx, []string{"repo-1"}, nil, nil); err == nil {
		t.Error("payload decode failure = nil, want wrapped error")
	}
}

// TestReadinessAuthorityProbeContract pins the probe's fallback polarity: a
// layout with the claim columns reads true, a schema without them reads
// false so the caller takes the legacy statement, and a probe failure
// surfaces so the caller fails closed to the protected statement.
func TestReadinessAuthorityProbeContract(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"layout and claim columns present", true},
		{"layout without claim columns", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			step := cov8AuthorityPresentStep()
			if !tc.want {
				step = cov8AuthorityAbsentStep()
			}
			tx := &cov8ImpactTx{steps: []cov8ImpactStep{step}}
			got, err := readinessAuthorityLayoutPresent(ctx, tx)
			if err != nil {
				t.Fatalf("probe() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("probe() = %v, want %v", got, tc.want)
			}
		})
	}
	want := errors.New("probe down")
	tx := &cov8ImpactTx{steps: []cov8ImpactStep{{err: want}}}
	if _, err := readinessAuthorityLayoutPresent(ctx, tx); !errors.Is(err, want) {
		t.Errorf("probe failure = %v, want wrap of %v", err, want)
	}
}
