package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// backfillTxDB adapts a single fakeExecQueryer into a transactional store so the
// batched deferred backfill (which opens a transaction per repository batch) can
// run against one ordered exec/query log. Begin returns a transaction that
// delegates to the same inner queryer; Commit/Rollback are no-ops. This keeps the
// backfill tests asserting on one execs slice while exercising the per-batch
// transaction path.
type backfillTxDB struct {
	inner      *fakeExecQueryer
	beginCalls int
}

func newBackfillTxDB(inner *fakeExecQueryer) *backfillTxDB {
	return &backfillTxDB{inner: inner}
}

func (db *backfillTxDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.inner.ExecContext(ctx, query, args...)
}

func (db *backfillTxDB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return db.inner.QueryContext(ctx, query, args...)
}

func (db *backfillTxDB) Begin(context.Context) (Transaction, error) {
	db.beginCalls++
	return &backfillTx{inner: db.inner}, nil
}

type backfillTx struct {
	inner *fakeExecQueryer
}

func (tx *backfillTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.inner.ExecContext(ctx, query, args...)
}

func (tx *backfillTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.inner.QueryContext(ctx, query, args...)
}

func (tx *backfillTx) Commit() error   { return nil }
func (tx *backfillTx) Rollback() error { return nil }

func TestIngestionStoreCommitScopeGenerationSkipsRelationshipBackfillWhenConfigured(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	envelopes := []facts.Envelope{{
		FactID:        "fact-1",
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "repository",
		StableFactKey: "repository:repo-123",
		ObservedAt:    generation.ObservedAt,
		Payload:       map[string]any{"graph_id": "repo-123"},
		SourceRef: facts.Ref{
			SourceSystem: "git",
			FactKey:      "fact-key",
		},
	}}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testFactChannel(envelopes)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	// Issue #3481/#3521: the repository catalog loads through the shared cache,
	// and the cold load runs on the OPEN ingestion transaction's connection (so a
	// single-connection pool cannot deadlock). With backfill skipped the only
	// transaction read is that catalog load, and the base connection issues none.
	if got, want := len(db.tx.queries), 1; got != want {
		t.Fatalf("transaction query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.tx.queries[0].query, "fact_kind = 'repository'") {
		t.Fatalf("transaction query = %q, want repository catalog load only", db.tx.queries[0].query)
	}
	if got, want := len(db.queries), 0; got != want {
		t.Fatalf("base connection query count = %d, want %d (catalog must not use a second connection)", got, want)
	}
}

// TestRepositoryScopedCatalogBoundsToNewRepos pins the scope-bounded contract
// (issue #3500): the per-commit relationship backfill must build its catalog
// matcher from only the repositories onboarded by the current generation, never
// the whole fleet. The returned scope is exactly the new-repo entries regardless
// of how large the refreshed catalog is, so matcher build and discovery cost
// scale with the onboarding delta, not O(all repositories).
func TestRepositoryScopedCatalogBoundsToNewRepos(t *testing.T) {
	t.Parallel()

	fleet := make([]relationships.CatalogEntry, 0, 1000)
	for i := 0; i < 1000; i++ {
		repoID := fmt.Sprintf("repo-%04d", i)
		fleet = append(fleet, relationships.CatalogEntry{
			RepoID:  repoID,
			Aliases: []string{repoID, fmt.Sprintf("org/%s", repoID)},
		})
	}
	newRepoIDs := map[string]struct{}{
		"repo-0007": {},
		"repo-0042": {},
	}

	scoped := repositoryScopedCatalog(fleet, newRepoIDs)

	if got, want := len(scoped), len(newRepoIDs); got != want {
		t.Fatalf("scoped catalog size = %d, want %d (must bound to new repos, not the %d-repo fleet)", got, want, len(fleet))
	}
	gotIDs := make([]string, 0, len(scoped))
	for _, entry := range scoped {
		if _, ok := newRepoIDs[entry.RepoID]; !ok {
			t.Fatalf("scoped catalog leaked non-new repo %q", entry.RepoID)
		}
		gotIDs = append(gotIDs, entry.RepoID)
		// Aliases must be preserved verbatim so matching truth is unchanged.
		if len(entry.Aliases) != 2 {
			t.Fatalf("scoped entry %q aliases = %v, want full alias set preserved", entry.RepoID, entry.Aliases)
		}
	}
	sort.Strings(gotIDs)
	if want := []string{"repo-0007", "repo-0042"}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("scoped catalog repo ids = %v, want %v", gotIDs, want)
	}
}

// TestBackfillScopedCatalogDiscoversSameEvidenceAsFullCatalog proves the
// scope-bounded backfill preserves correlation truth (issue #3500 accuracy
// gate): discovering evidence with the new-repo-scoped catalog yields exactly
// the same evidence that the prior full-catalog-then-filter path produced. The
// fleet here carries many unrelated repos plus the source facts that reference
// one newly onboarded target; both paths must agree edge-for-edge.
func TestBackfillScopedCatalogDiscoversSameEvidenceAsFullCatalog(t *testing.T) {
	t.Parallel()

	fleet := []relationships.CatalogEntry{
		{RepoID: "repo-app", Aliases: []string{"app-repo"}},
		{RepoID: "repo-infra", Aliases: []string{"infra-repo"}},
		{RepoID: "repo-unrelated-1", Aliases: []string{"unrelated-1"}},
		{RepoID: "repo-unrelated-2", Aliases: []string{"unrelated-2"}},
	}
	// Source facts from a pre-existing infra repo that reference the newly
	// onboarded app repo via Terraform content.
	sourceFacts := []facts.Envelope{{
		FactKind: "content",
		ScopeID:  "scope-infra",
		Payload: map[string]any{
			"repo_id":       "repo-infra",
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			"content":       `app_repo = "app-repo"` + "\n" + `unrelated = "unrelated-1"`,
		},
	}}
	newRepoIDs := map[string]struct{}{"repo-app": {}}

	// Reference: prior full-catalog discovery, then filter to the new repos.
	reference := filterEvidenceByTargetRepo(
		relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(sourceFacts, fleet)),
		newRepoIDs,
	)
	// New: scope the catalog to the new repos before discovery (no post-filter).
	scoped := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(sourceFacts, repositoryScopedCatalog(fleet, newRepoIDs)),
	)

	if !reflect.DeepEqual(scoped, reference) {
		t.Fatalf("scoped backfill evidence diverged from full-catalog reference:\nscoped    = %#v\nreference = %#v", scoped, reference)
	}
	if len(scoped) == 0 {
		t.Fatal("expected at least one evidence fact targeting the onboarded repo")
	}
	for _, fact := range scoped {
		if fact.TargetRepoID != "repo-app" {
			t.Fatalf("scoped evidence targeted %q, want only the onboarded repo-app", fact.TargetRepoID)
		}
	}
}

// newBackfillScaleCorpus builds a fleet of fleetSize repositories and a set of
// source content facts that reference the two onboarded target repos, for the
// scope-bounded backfill scale benchmarks (issue #3500). The onboarding delta is
// fixed at two repos regardless of fleetSize, so the benchmarks isolate how
// backfill cost responds to fleet growth.
func newBackfillScaleCorpus(fleetSize int) ([]relationships.CatalogEntry, []facts.Envelope, map[string]struct{}) {
	fleet := make([]relationships.CatalogEntry, 0, fleetSize)
	for i := 0; i < fleetSize; i++ {
		repoID := fmt.Sprintf("repo-%05d", i)
		fleet = append(fleet, relationships.CatalogEntry{
			RepoID:  repoID,
			Aliases: []string{repoID, fmt.Sprintf("org/%s", repoID)},
		})
	}
	newRepoIDs := map[string]struct{}{"repo-00007": {}, "repo-00042": {}}

	// One source content fact per fleet repo, mirroring the corpus-wide fact
	// load the backfill performs. A handful reference the onboarded repos.
	sourceFacts := make([]facts.Envelope, 0, fleetSize)
	for i := 0; i < fleetSize; i++ {
		repoID := fmt.Sprintf("repo-%05d", i)
		content := fmt.Sprintf("module_source = %q", repoID)
		if i%500 == 0 {
			content = `app = "repo-00007"` + "\n" + `infra = "repo-00042"`
		}
		sourceFacts = append(sourceFacts, facts.Envelope{
			FactKind: "content",
			ScopeID:  "scope-" + repoID,
			Payload: map[string]any{
				"repo_id":       repoID,
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       content,
			},
		})
	}
	return fleet, sourceFacts, newRepoIDs
}

// BenchmarkBackfillDiscoveryFullCatalog measures the prior per-commit backfill
// shape: discover evidence against the whole fleet catalog, then filter to the
// onboarded repos. Matcher build and per-fact match cost grow with fleet size,
// so this benchmark regresses as the fleet scales (the O(all-repos) behavior
// issue #3500 removes).
func BenchmarkBackfillDiscoveryFullCatalog(b *testing.B) {
	for _, fleetSize := range []int{1000, 5000} {
		fleet, sourceFacts, newRepoIDs := newBackfillScaleCorpus(fleetSize)
		b.Run(fmt.Sprintf("fleet=%d", fleetSize), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = filterEvidenceByTargetRepo(
					relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(sourceFacts, fleet)),
					newRepoIDs,
				)
			}
		})
	}
}

// BenchmarkBackfillDiscoveryScoped measures the scope-bounded backfill shape
// (issue #3500): discover evidence against only the onboarded repos' catalog.
// Matcher build cost is bounded by the onboarding delta, so per-fact match cost
// stays flat as the fleet scales from 1k to 5k repos.
func BenchmarkBackfillDiscoveryScoped(b *testing.B) {
	for _, fleetSize := range []int{1000, 5000} {
		fleet, sourceFacts, newRepoIDs := newBackfillScaleCorpus(fleetSize)
		scoped := repositoryScopedCatalog(fleet, newRepoIDs)
		b.Run(fmt.Sprintf("fleet=%d", fleetSize), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = relationships.DedupeEvidenceFacts(
					relationships.DiscoverEvidence(sourceFacts, scoped),
				)
			}
		})
	}
}

func TestIngestionStoreBackfillAllRelationshipEvidenceSkipsUnknownTargetGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	otherGen := [][]any{
		{"repo-other", "scope-other", "gen-other"},
	}
	otherPartitions := [][]any{
		{"scope-other", "gen-other"},
	}
	inner := &fakeExecQueryer{
		// Per-scope deferred fact load (issue #3710): the source fact lives in
		// scope-infra/gen-infra but references repo-app. Its target generation is
		// unknown to the write path (active generations only know repo-other), so no
		// evidence is persisted but readiness is still published.
		deferredFactsByScope: map[string][][]any{
			"scope-other": {
				{
					"fact-1",
					"scope-infra",
					"gen-infra",
					"content",
					"content:1",
					"content.v1",
					"git",
					int64(0),
					"unknown",
					"git",
					"source-fact-1",
					"",
					"",
					now,
					false,
					[]byte(`{"artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`),
				},
			},
		},
		queryResponses: []queueFakeRows{
			// catalog
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// scope-generation partition snapshot (fact-load partitioning, #3710)
			{rows: otherPartitions},
			// active repository generations snapshot (write phase)
			{rows: otherGen},
			// batch transaction re-load of active generations under the lock
			{rows: otherGen},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO relationship_evidence_facts") {
			t.Fatalf("unexpected evidence insert for unknown target generation:\n%s", execCall.query)
		}
	}
	foundPhasePublish := false
	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO graph_projection_phase_state") {
			foundPhasePublish = true
			break
		}
	}
	if !foundPhasePublish {
		t.Fatal("expected backward evidence readiness publish")
	}
}

func TestIngestionStoreBackfillAllRelationshipEvidencePersistsBySourceGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-infra", "scope-infra", "gen-infra"},
		{"repo-app", "scope-app", "gen-app"},
	}
	scopeGenPartitions := [][]any{
		{"scope-infra", "gen-infra"},
		{"scope-app", "gen-app"},
	}
	inner := &fakeExecQueryer{
		// Per-scope deferred fact load (issue #3710): the infra source fact lives in
		// scope-infra/gen-infra and references repo-app, so evidence attaches to the
		// infra source generation.
		deferredFactsByScope: map[string][][]any{
			"scope-infra": {
				{
					"fact-1",
					"scope-infra",
					"gen-infra",
					"content",
					"content:1",
					"content.v1",
					"git",
					int64(0),
					"unknown",
					"git",
					"source-fact-1",
					"",
					"",
					now,
					false,
					[]byte(`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`),
				},
			},
		},
		queryResponses: []queueFakeRows{
			// catalog
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// scope-generation partition snapshot (fact-load partitioning, #3710)
			{rows: scopeGenPartitions},
			// active repository generations snapshot (write phase)
			{rows: activeGens},
			// batch transaction re-load of active generations under the lock
			{rows: activeGens},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	var evidenceInserts []fakeExecCall
	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO relationship_evidence_facts") {
			evidenceInserts = append(evidenceInserts, execCall)
		}
	}
	if len(evidenceInserts) != 1 {
		t.Fatalf("relationship evidence inserts = %d, want 1", len(evidenceInserts))
	}
	if got, want := evidenceInserts[0].args[1], "gen-infra"; got != want {
		t.Fatalf("evidence generation_id = %v, want source generation %q", got, want)
	}
}
