// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9049 disposable PostgreSQL proof that the operator credential purge (statement A run verbatim from docs/public/deployment/credential-file-cleanup.md) invalidates the cached semantic/hybrid index: a cached search, then the purge, then the same search on the same process returns no deleted context_text with no restart; isolated rows in a disposable database
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// purgeProbeCleanupPage is the operator page whose purge SQL this proof runs
// verbatim, so the tested invalidation is the invalidation operators get.
const purgeProbeCleanupPage = "../../../../docs/public/deployment/credential-file-cleanup.md"

var (
	purgeProbeFencePattern = regexp.MustCompile("(?s)```sql\n(.*?)```")
	purgeProbeHeadPattern  = regexp.MustCompile(`(?m)^-- Statement ([A-Za-z]+):`)
	purgeProbeVarPattern   = regexp.MustCompile(`:'([a-z]+)'`)
)

// TestSemanticSearchCacheInvalidatedByCredentialPurgeLive is the #9049
// acceptance test: one cached search, then the runbook purge, then the same
// search on the same hybrid (no restart, warm cache) must serve no deleted
// context_text, and the snapshot key must have moved.
func TestSemanticSearchCacheInvalidatedByCredentialPurgeLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := pgstatus.ApplyBootstrap(ctx, pgstatus.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	scopeID := "scope-purge-9049-" + suffix
	repoID := "repo-purge-9049-" + suffix
	generation := scopeID + "-gen"
	w := time.Now().UTC().Truncate(time.Second)
	ingested := w.Add(-time.Hour)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %.80q: %v", query, err)
		}
	}
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)`, scopeID, ingested)
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $3, 'active')`, generation, scopeID, ingested)
	exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generation, scopeID)
	exec(`INSERT INTO eshu_search_document_projection_state
    (scope_id, generation_id, projection_revision, build_fence, state, document_count, updated_at)
VALUES ($1, $2, 1, 1, 'ready', 1, $3)`, scopeID, generation, w)
	exec(`INSERT INTO eshu_search_vector_scope_state
    (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id,
     vector_index_version, projection_revision, build_fence, document_cursor, state, updated_at)
VALUES ($1, $2, 'local', 'search_documents', 'local-hash-v1', 'vector-v1', 1, 1, '', 'ready', $3)`,
		scopeID, generation, w)

	const seed = "PurgeProbeSeed9049"
	docID := "searchdoc:purge-probe-" + suffix
	factID := "fact-purge-9049-" + suffix
	exec(`INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
    source_kind, document, document_length, updated_at)
VALUES ($1, $2, $3, $4, $5, 'content_file',
    jsonb_build_object('Path', '.yarnrc.yml', 'ContextText', $6::text), 2, now())`,
		scopeID, generation, docID, factID, repoID, "refundable token "+seed)
	// The hybrid serves curated documents from the reducer fact; statement B
	// of the purge deletes that fact row.
	exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'reducer_eshu_search_document', $1, 'git', $1, now(), now(),
    jsonb_build_object('repo_id', $4::text, 'document', jsonb_build_object(
        'ID', $5::text, 'RepoID', $4::text, 'Title', 'yarnrc', 'Path', '.yarnrc.yml', 'ContextText', $6::text)))`,
		factID, scopeID, generation, repoID, docID, "refundable token "+seed)

	embedder, err := searchembed.NewHashEmbedder(searchembed.DefaultDimensions)
	if err != nil {
		t.Fatalf("create hash embedder: %v", err)
	}
	// Read the document back through the production reader so the seeded
	// vector hash covers exactly the text the hybrid will index. This read
	// bypasses the counting wrapper so the first hybrid search is load one.
	seeded, err := NewPostgresSemanticSearchIndexStore(db).ListActiveDocuments(ctx, semanticSearchDocumentQuery{
		ScopeID: scopeID, RepoID: repoID, Limit: 10,
	})
	documents := &liveCountingSemanticSearchDocumentStore{inner: NewPostgresSemanticSearchIndexStore(db)}
	if err != nil {
		t.Fatalf("list seeded documents: %v", err)
	}
	if len(seeded) != 1 {
		t.Fatalf("seeded documents = %d, want 1", len(seeded))
	}
	hash := searchhybrid.DocumentContentHash(seeded[0].Document)
	vector, err := embedder.Embed(ctx, searchhybrid.DocumentText(seeded[0].Document))
	if err != nil {
		t.Fatalf("embed seeded document: %v", err)
	}
	literals := make([]string, 0, len(vector))
	for _, value := range vector {
		literals = append(literals, strconv.FormatFloat(value, 'g', -1, 64))
	}
	vecLit := "{" + strings.Join(literals, ",") + "}"
	dims := embedder.Dimensions()
	exec(`INSERT INTO eshu_search_vector_metadata (scope_id, generation_id, document_id, provider_profile_id,
    source_class, embedding_model_id, embedding_dimensions, embedding_content_hash,
    vector_index_version, build_state, created_at, updated_at)
VALUES ($1, $2, $3, 'local', 'search_documents', 'local-hash-v1', $4, $5, 'vector-v1', 'ready', now(), now())`,
		scopeID, generation, docID, dims, hash)
	exec(`INSERT INTO eshu_search_vector_values (scope_id, generation_id, document_id, provider_profile_id,
    source_class, embedding_model_id, embedding_dimensions, embedding_content_hash,
    vector_index_version, vector_values, created_at, updated_at)
VALUES ($1, $2, $3, 'local', 'search_documents', 'local-hash-v1', $4, $5, 'vector-v1', $6::double precision[], now(), now())`,
		scopeID, generation, docID, dims, hash, vecLit)

	metadata := &liveCountingSemanticSearchMetadataStore{
		inner: pgstatus.NewEshuSearchVectorMetadataStore(pgstatus.SQLDB{DB: db}),
	}
	values := &liveCountingSemanticSearchValueStore{
		inner: pgstatus.NewEshuSearchVectorValueStore(pgstatus.SQLDB{DB: db}),
	}
	snapshots := &liveCountingSemanticSearchSnapshotStore{
		inner: NewPostgresSemanticSearchSnapshotStore(pgstatus.SQLDB{DB: db}),
	}
	hybrid := NewCachedPersistedLocalSemanticSearchHybrid(
		documents, metadata, values, snapshots, embedder,
		DefaultPersistedLocalSemanticSearchHybridConfig(),
	)
	query := SemanticSearchIndexQuery{
		Request: searchretrieval.Request{
			Query: "refundable token " + seed, Scope: searchretrieval.Scope{RepoID: repoID},
			Mode: searchbench.ModeSemantic, Limit: 5, Timeout: 5 * time.Second,
		},
		ScopeID: scopeID,
		RepoID:  repoID,
	}
	containsSeed := func(result SemanticSearchIndexResult) bool {
		for _, candidate := range result.Candidates {
			if strings.Contains(candidate.Document.ContextText, seed) {
				return true
			}
		}
		return false
	}
	first, err := hybrid.Search(ctx, query)
	if err != nil {
		t.Fatalf("first Search() error = %v", err)
	}
	if !containsSeed(first) {
		t.Fatalf("first search serves no candidate with the seeded context text")
	}
	if documents.calls != 1 {
		t.Fatalf("document loads after first search = %d, want 1 cached miss", documents.calls)
	}
	before, err := snapshots.inner.Load(ctx, hybrid.snapshotRequest(scopeID))
	if err != nil {
		t.Fatalf("load snapshot before purge: %v", err)
	}

	// Run the operator purge verbatim: the page's statements A and B, bound
	// the way psql -v binds them, each until it touches zero rows.
	purges := []string{purgeProbeStatement(t, "A"), purgeProbeStatement(t, "B")}
	vars := map[string]string{
		"repo": repoID,
		"pred": `(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$`,
		"w":    w.Format(time.RFC3339),
	}
	for _, purge := range purges {
		for i := 0; ; i++ {
			if touched := purgeProbeRunScript(t, ctx, db, purge, vars); touched == 0 {
				break
			}
			if i > 20 {
				t.Fatal("purge statement still touches rows after 20 passes")
			}
		}
	}
	var remaining int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM eshu_search_index_documents
WHERE scope_id = $1 AND generation_id = $2`, scopeID, generation).Scan(&remaining); err != nil {
		t.Fatalf("count documents after purge: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("documents after purge = %d, want 0", remaining)
	}
	var facts int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = 'reducer_eshu_search_document'`,
		scopeID, generation).Scan(&facts); err != nil {
		t.Fatalf("count facts after purge: %v", err)
	}
	if facts != 0 {
		t.Fatalf("facts after purge = %d, want 0", facts)
	}

	// Same process, warm cache, no restart: the deleted text must be gone.
	second, err := hybrid.Search(ctx, query)
	if err != nil {
		t.Fatalf("second Search() error = %v", err)
	}
	if containsSeed(second) {
		t.Fatalf("search after the purge still serves the deleted context text with no restart")
	}
	if documents.calls != 2 {
		t.Fatalf("document loads after purge = %d, want 2 (the purge must invalidate the cached index)", documents.calls)
	}
	after, err := snapshots.inner.Load(ctx, hybrid.snapshotRequest(scopeID))
	if err != nil {
		t.Fatalf("load snapshot after purge: %v", err)
	}
	if after == before {
		t.Fatalf("snapshot after purge = %+v, want a moved cache key (was %+v)", after, before)
	}
	if after.DocumentProjectionRevision == before.DocumentProjectionRevision {
		t.Fatalf("document revision after purge = %d, want it bumped from %d",
			after.DocumentProjectionRevision, before.DocumentProjectionRevision)
	}
	if after.VectorProjectionRevision != after.DocumentProjectionRevision {
		t.Fatalf("vector revision %d does not match document revision %d after the purge",
			after.VectorProjectionRevision, after.DocumentProjectionRevision)
	}
}

// purgeProbeStatement returns one purge statement from the operator page,
// verbatim, so the proof cannot drift from what operators copy.
func purgeProbeStatement(t *testing.T, name string) string {
	t.Helper()
	page, err := os.ReadFile(purgeProbeCleanupPage)
	if err != nil {
		t.Fatalf("read %s: %v", purgeProbeCleanupPage, err)
	}
	fences := purgeProbeFencePattern.FindAllStringSubmatch(string(page), -1)
	if len(fences) != 2 {
		t.Fatalf("%s holds %d sql fences, want the census and the purge", purgeProbeCleanupPage, len(fences))
	}
	purge := fences[1][1]
	locations := purgeProbeHeadPattern.FindAllStringSubmatchIndex(purge, -1)
	sections := map[string]string{}
	for i, location := range locations {
		end := len(purge)
		if i+1 < len(locations) {
			end = locations[i+1][0]
		}
		sections[purge[location[2]:location[3]]] = purge[location[0]:end]
	}
	statement, ok := sections[name]
	if !ok {
		t.Fatalf("purge fence holds no statement %q", name)
	}
	return statement
}

// purgeProbeRunScript binds psql :'variables' the way psql -v does, runs each
// statement on one connection like psql -f, and returns the rows the
// data-modifying statements touched.
func purgeProbeRunScript(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	script string,
	vars map[string]string,
) int64 {
	t.Helper()
	bound := purgeProbeVarPattern.ReplaceAllStringFunc(script, func(match string) string {
		name := purgeProbeVarPattern.FindStringSubmatch(match)[1]
		value, ok := vars[name]
		if !ok {
			t.Fatalf("statement uses psql variable %q, which the test does not bind", name)
		}
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	})
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var touched int64
	for _, part := range strings.Split(bound, ";") {
		statement := strings.TrimSpace(part)
		if statement == "" {
			continue
		}
		result, err := conn.ExecContext(ctx, statement)
		if err != nil {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
			t.Fatalf("run %.80q: %v", statement, err)
		}
		head := strings.ToUpper(statement)
		if strings.HasPrefix(head, "UPDATE") || strings.HasPrefix(head, "DELETE") ||
			strings.HasPrefix(head, "WITH") {
			n, _ := result.RowsAffected()
			touched += n
		}
	}
	return touched
}
