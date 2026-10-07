// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// Harness for superseded_delta_overlay_live_test.go (#7389). It replaces only
// the git collector: generations are committed through the production
// IngestionStore with the collector's own envelope builders, and the projector
// Service and canonical writer are the production ones.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	gitcollector "github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// overlayFile is one file in a commit's tree.
type overlayFile struct{ rel, body string }

// overlayGen is one generation the git collector would commit.
type overlayGen struct {
	id, commit, baseline string
	files                []overlayFile // full: whole tree; delta: changed files only
	changed, deleted     []string      // delta_relative_paths / delta_deleted_relative_paths
	delta                bool
}

// gatedContentWriter wraps the production content writer. For gatedID only it
// runs the real write and then calls after once; a nil after is a no-op.
type gatedContentWriter struct {
	inner   content.Writer
	gatedID string
	after   func(context.Context) error
	fired   atomic.Bool
}

func (w *gatedContentWriter) Write(ctx context.Context, mat content.Materialization) (content.Result, error) {
	result, err := w.inner.Write(ctx, mat)
	if err != nil || mat.GenerationID != w.gatedID || !w.fired.CompareAndSwap(false, true) {
		return result, err
	}
	if w.after != nil {
		if hookErr := w.after(ctx); hookErr != nil {
			return content.Result{}, hookErr
		}
	}
	return result, nil
}

// survivesHeartbeats stands in for projection work still running (intent
// enqueue) after the content write, across several heartbeat ticks. It
// returns ctx's error if a heartbeat canceled the projection, which the
// write-through contract forbids once the write-start marker is set.
func survivesHeartbeats(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("projection canceled after its write started: %w", ctx.Err())
	case <-time.After(d):
		return nil
	}
}

// gatedCanonicalWriter wraps the production canonical writer. For gatedID only
// it calls before, runs the real write, then calls after, once.
type gatedCanonicalWriter struct {
	inner         runtime.CanonicalWriter
	gatedID       string
	before, after func(context.Context) error
	fired         atomic.Bool
}

func (w *gatedCanonicalWriter) Write(ctx context.Context, mat canonical.CanonicalMaterialization) error {
	if mat.GenerationID != w.gatedID || !w.fired.CompareAndSwap(false, true) {
		return w.inner.Write(ctx, mat)
	}
	if err := w.before(ctx); err != nil {
		return err
	}
	if err := w.inner.Write(ctx, mat); err != nil {
		return err
	}
	return w.after(ctx)
}

// hookedHeartbeater calls before ahead of the real heartbeat while armed.
type hookedHeartbeater struct {
	inner  projector.ProjectorWorkHeartbeater
	armed  atomic.Bool
	before func(context.Context) error
}

func (h *hookedHeartbeater) Heartbeat(ctx context.Context, work projector.ScopeGenerationWork) error {
	if h.armed.CompareAndSwap(true, false) {
		if err := h.before(ctx); err != nil {
			return err
		}
	}
	return h.inner.Heartbeat(ctx, work)
}

type overlayHarness struct {
	t        *testing.T
	sqlDB    *sql.DB
	pgDB     postgres.SQLDB
	store    postgres.IngestionStore
	driver   neo4jdriver.DriverWithContext
	database string
	repo     repositoryidentity.Metadata
	scope    scope.IngestionScope
	seq      time.Time
	getenv   func(string) string
	mu       sync.Mutex
	gens     map[string]scope.ScopeGeneration
}

func newOverlayHarness(ctx context.Context, t *testing.T, dsn string) *overlayHarness {
	t.Helper()
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, stmt := range []string{"DROP SCHEMA IF EXISTS public CASCADE", "CREATE SCHEMA public"} {
		if _, err := sqlDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("reset proof database (%s): %v", stmt, err)
		}
	}
	pgDB := postgres.SQLDB{DB: sqlDB}
	if err := postgres.ApplyBootstrap(ctx, pgDB); err != nil {
		t.Fatalf("apply postgres bootstrap: %v", err)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	driver, err := neo4jdriver.NewDriverWithContext(os.Getenv("ESHU_NEO4J_URI"),
		neo4jdriver.BasicAuth(os.Getenv("ESHU_NEO4J_USERNAME"), os.Getenv("ESHU_NEO4J_PASSWORD"), ""))
	if err != nil {
		t.Fatalf("open neo4j driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	repo, err := repositoryidentity.MetadataFor("proof-7389", "/repos/proof-7389",
		"https://github.com/eshu-proof/proof-7389.git")
	if err != nil {
		t.Fatalf("repository identity: %v", err)
	}
	env := map[string]string{
		"ESHU_GRAPH_BACKEND": "neo4j", "ESHU_NEO4J_URI": os.Getenv("ESHU_NEO4J_URI"),
		"ESHU_NEO4J_USERNAME": os.Getenv("ESHU_NEO4J_USERNAME"), "ESHU_NEO4J_PASSWORD": os.Getenv("ESHU_NEO4J_PASSWORD"),
		"ESHU_NEO4J_DATABASE": database, "ESHU_PROJECTOR_WORKERS": "1",
	}
	h := &overlayHarness{
		t: t, sqlDB: sqlDB, pgDB: pgDB, store: postgres.NewIngestionStore(pgDB),
		driver: driver, database: database, repo: repo, seq: time.Now().UTC().Add(-time.Hour),
		getenv: func(key string) string { return env[key] },
		gens:   map[string]scope.ScopeGeneration{},
	}
	// Mirrors the git collector's buildScope for a default-branch repository.
	h.scope = scope.IngestionScope{
		ScopeID: "git-repository-scope:" + repo.ID, SourceSystem: "git", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorGit, PartitionKey: repo.ID,
		Metadata: map[string]string{
			"repo_id": repo.ID, "repo_name": repo.Name, "source_key": repo.ID,
			"remote_url": repo.RemoteURL, "local_path": repo.LocalPath,
		},
	}
	h.runCypher(ctx, "MATCH (n) DETACH DELETE n", nil)
	t.Cleanup(func() { h.runCypher(context.Background(), "MATCH (n) DETACH DELETE n", nil) })
	return h
}

// buildService returns the production projector Service and the production
// canonical writer it wraps.
func (h *overlayHarness) buildService(ctx context.Context) (projector.Service, runtime.CanonicalWriter) {
	h.t.Helper()
	writer, closer, err := openProjectorCanonicalWriter(ctx, h.pgDB, h.getenv, nil, nil)
	if err != nil {
		h.t.Fatalf("open production canonical writer: %v", err)
	}
	h.t.Cleanup(func() { _ = closer.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	svc, err := buildProjectorService(h.pgDB, writer, h.getenv, nil, nil, logger)
	if err != nil {
		h.t.Fatalf("build production projector service: %v", err)
	}
	svc.PollInterval = 50 * time.Millisecond
	return svc, writer
}

// run starts svc until the test ends.
func (h *overlayHarness) run(ctx context.Context, svc projector.Service) {
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- svc.Run(runCtx) }()
	h.t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			h.t.Errorf("projector service returned: %v", err)
		}
	})
}

// commit persists gen through the production IngestionStore with the facts
// the git collector's envelope builders emit.
func (h *overlayHarness) commit(ctx context.Context, gen overlayGen) error {
	h.t.Helper()
	h.mu.Lock()
	h.seq = h.seq.Add(time.Second)
	observed := h.seq
	h.mu.Unlock()
	generation := scope.ScopeGeneration{
		GenerationID: gen.id, ScopeID: h.scope.ScopeID, ObservedAt: observed, IngestedAt: observed,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: h.freshnessHint(ctx, gen), SourceCommitSHA: gen.commit, IsDelta: gen.delta,
		DeltaBaselineCommitSHA: gen.baseline,
	}
	h.mu.Lock()
	h.gens[gen.id] = generation
	h.mu.Unlock()
	repoPath := h.repo.LocalPath
	envelopes := []facts.Envelope{gitcontent.RepositoryFactEnvelope(
		repoPath, h.repo, "run-"+gen.id, h.scope.ScopeID, gen.id, observed, len(gen.files),
		nil, false, "main", nil, gen.delta, gen.changed, gen.deleted, false,
	)}
	for _, file := range gen.files {
		envelopes = append(envelopes,
			gitcontent.FileFactEnvelope(repoPath, h.repo.ID, h.scope.ScopeID, gen.id, observed, map[string]any{
				"path": repoPath + "/" + file.rel, "language": "go",
			}, false),
			gitcontent.ContentFactEnvelope(repoPath, h.repo.ID, h.scope.ScopeID, gen.id, observed, model.ContentFileSnapshot{
				RelativePath: file.rel, Body: file.body, Digest: fmt.Sprintf("digest-%s-%s", gen.id, file.rel),
				Language: "go", CommitSHA: gen.commit,
			}),
		)
	}
	// The collector's fileTombstoneEnvelope/contentTombstoneEnvelope for each
	// deleted path (fact_builder_delta.go), rebuilt here because they are
	// unexported in package git.
	for _, rel := range gen.deleted {
		fileTombstone := model.FactEnvelope("file", h.scope.ScopeID, gen.id, observed, "file:"+h.repo.ID+":"+rel,
			map[string]any{
				"graph_id": h.repo.ID + ":" + rel, "graph_kind": "file", "repo_id": h.repo.ID,
				"relative_path": rel, "is_dependency": false,
			}, repoPath+"/"+rel)
		fileTombstone.IsTombstone = true
		contentTombstone := model.FactEnvelope("content", h.scope.ScopeID, gen.id, observed, "content:"+h.repo.ID+":"+rel,
			map[string]any{"content_path": rel, "content_digest": "", "repo_id": h.repo.ID}, repoPath+"/"+rel)
		contentTombstone.IsTombstone = true
		envelopes = append(envelopes, fileTombstone, contentTombstone)
	}
	stream := make(chan facts.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		stream <- envelope
	}
	close(stream)
	if err := h.store.CommitScopeGeneration(ctx, h.scope, generation, stream); err != nil {
		h.t.Errorf("commit %s: %v", gen.id, err)
		return err
	}
	return nil
}

func (h *overlayHarness) waitGeneration(ctx context.Context, generationID, status string) {
	h.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		_ = h.sqlDB.QueryRowContext(ctx, `SELECT status FROM scope_generations WHERE generation_id = $1`,
			generationID).Scan(&got)
		if got == status {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.logGenerations(ctx)
	h.t.Fatalf("generation %s status = %q, want %q", generationID, got, status)
}

func (h *overlayHarness) logGenerations(ctx context.Context) {
	h.t.Helper()
	rows, err := h.sqlDB.QueryContext(ctx, `
SELECT g.generation_id, g.status, g.is_delta, left(COALESCE(g.delta_baseline_commit_sha, ''), 1),
       left(g.source_commit_sha, 1), g.activated_at IS NOT NULL,
       COALESCE(w.status, ''), COALESCE(w.failure_class, '')
FROM scope_generations g
LEFT JOIN fact_work_items w ON w.generation_id = g.generation_id AND w.stage = 'projector'
WHERE g.scope_id = $1 ORDER BY g.ingested_at`, h.scope.ScopeID)
	if err != nil {
		h.t.Fatalf("read generations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, status, baseline, commit, workStatus, class string
		var delta, activated bool
		if err := rows.Scan(&id, &status, &delta, &baseline, &commit, &activated, &workStatus, &class); err != nil {
			h.t.Fatalf("scan generation: %v", err)
		}
		h.t.Logf("  generation %-16s status=%-10s delta=%-5t commit=%s baseline=%-1s ever_activated=%-5t work=%s failure_class=%s",
			id, status, delta, commit, baseline, activated, workStatus, class)
	}
	var active string
	_ = h.sqlDB.QueryRowContext(ctx, `SELECT COALESCE(active_generation_id, '') FROM ingestion_scopes WHERE scope_id = $1`,
		h.scope.ScopeID).Scan(&active)
	h.t.Logf("  ingestion_scopes.active_generation_id=%s", active)
}

// graphFiles returns relative_path -> generation_id for the repository's File
// nodes reached through REPO_CONTAINS.
func (h *overlayHarness) graphFiles(ctx context.Context) map[string]string {
	h.t.Helper()
	records := h.runCypher(ctx, `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
RETURN f.relative_path AS rel, f.generation_id AS gen`, map[string]any{"repo_id": h.repo.ID})
	out := make(map[string]string, len(records))
	for _, record := range records {
		rel, _ := record.Get("rel")
		gen, _ := record.Get("gen")
		out[fmt.Sprint(rel)] = fmt.Sprint(gen)
	}
	return out
}

func (h *overlayHarness) logState(ctx context.Context, label string) {
	h.t.Helper()
	files := h.graphFiles(ctx)
	keys := sortedKeys(files)
	h.t.Logf("graph %s: %d File nodes under Repository via REPO_CONTAINS", label, len(keys))
	for _, key := range keys {
		h.t.Logf("  File relative_path=%-8s generation_id=%s", key, files[key])
	}
	rows, err := h.sqlDB.QueryContext(ctx, `SELECT relative_path, COALESCE(left(commit_sha, 1), '')
FROM content_files WHERE repo_id = $1 ORDER BY relative_path`, h.repo.ID)
	if err != nil {
		h.t.Fatalf("read content_files: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var entries []string
	for rows.Next() {
		var rel, commit string
		if err := rows.Scan(&rel, &commit); err != nil {
			h.t.Fatalf("scan content row: %v", err)
		}
		entries = append(entries, rel+"@"+commit)
	}
	h.t.Logf("  content_files (path@commit): %v", entries)
}

func (h *overlayHarness) checkFiles(ctx context.Context, label string, want []string) bool {
	h.t.Helper()
	got := sortedKeys(h.graphFiles(ctx))
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		h.t.Errorf("%s: graph files = %v, want %v", label, got, want)
		return false
	}
	return true
}

func (h *overlayHarness) assertFiles(ctx context.Context, label string, want []string) {
	h.t.Helper()
	if !h.checkFiles(ctx, label, want) {
		h.t.FailNow()
	}
}

func (h *overlayHarness) runCypher(ctx context.Context, cypher string, params map[string]any) []*neo4jdriver.Record {
	h.t.Helper()
	session := h.driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: h.database})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		h.t.Fatalf("cypher %q: %v", cypher, err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		h.t.Fatalf("collect %q: %v", cypher, err)
	}
	return records
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// overlayContentHint is a content-derived freshness hint, as the collector's
// is: identical file sets (G_C and the heal full at C, both tree A) share it,
// so the ingestion store's unchanged-generation skip applies to them.
func overlayContentHint(gen overlayGen) string {
	parts := make([]string, 0, len(gen.files)+len(gen.deleted))
	for _, file := range gen.files {
		parts = append(parts, file.rel+"\x00"+file.body)
	}
	for _, rel := range gen.deleted {
		parts = append(parts, "deleted\x00"+rel)
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return hex.EncodeToString(sum[:])
}

// freshnessHint derives gen's freshness hint the way the git collector does.
// A full generation asks the production reconcile decision (the sweep plus the
// #7389 graph_dirty reason, on the production IngestionStore resolver) whether
// it is a reconciliation snapshot, and the production fact-builder rule then
// blanks the hint for one. A delta never carries the reconcile flag.
func (h *overlayHarness) freshnessHint(ctx context.Context, gen overlayGen) string {
	hint := overlayContentHint(gen)
	if gen.delta {
		return hint
	}
	due, reason := gitcollector.ReconcileSweepDecision(ctx, h.store, 24*time.Hour, time.Time{}, time.Time{}, time.Now().UTC(), h.scope.ScopeID, nil)
	h.t.Logf("reconcile decision for full %s: due=%t reason=%s", gen.id, due, reason)
	return gitcollector.GenerationFreshnessHint(hint, due)
}

// workFailureClass returns the projector work row's failure_class for id.
func (h *overlayHarness) workFailureClass(ctx context.Context, id string) string {
	h.t.Helper()
	var class string
	if err := h.sqlDB.QueryRowContext(ctx, `SELECT COALESCE(failure_class, '') FROM fact_work_items
WHERE generation_id = $1 AND stage = 'projector'`, id).Scan(&class); err != nil {
		h.t.Fatalf("read %s failure_class: %v", id, err)
	}
	return class
}

// writeStarted reports whether id's #7389 write-start marker is set.
func (h *overlayHarness) writeStarted(ctx context.Context, id string) bool {
	h.t.Helper()
	var started bool
	if err := h.sqlDB.QueryRowContext(ctx, `SELECT projection_write_started_at IS NOT NULL
FROM scope_generations WHERE generation_id = $1`, id).Scan(&started); err != nil {
		h.t.Fatalf("read %s write start: %v", id, err)
	}
	return started
}

// uncoveredWriters returns the production UncoveredProjectionWriters ids.
func (h *overlayHarness) uncoveredWriters(ctx context.Context) []string {
	h.t.Helper()
	writers, err := h.store.UncoveredProjectionWriters(ctx, h.scope.ScopeID)
	if err != nil {
		h.t.Fatalf("UncoveredProjectionWriters(): %v", err)
	}
	ids := make([]string, 0, len(writers))
	for _, writer := range writers {
		ids = append(ids, writer.GenerationID)
	}
	return ids
}

// generation returns the ScopeGeneration commit persisted for id.
func (h *overlayHarness) generation(id string) scope.ScopeGeneration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.gens[id]
}
