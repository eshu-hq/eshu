// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWaitForContentSearchReadySucceedsAfterRetries(t *testing.T) {
	t.Parallel()

	calls := 0
	probe := func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("content substring indexes are not ready")
		}
		return nil
	}
	if err := waitForContentSearchReady(context.Background(), probe, time.Minute, time.Millisecond); err != nil {
		t.Fatalf("waitForContentSearchReady() = %v, want nil after the probe turns ready", err)
	}
	if calls != 3 {
		t.Fatalf("probe calls = %d, want 3 (fail fast on first success, no extra polls)", calls)
	}
}

func TestWaitForContentSearchReadyTimesOut(t *testing.T) {
	t.Parallel()

	probe := func(context.Context) error { return errors.New("content substring indexes are not ready") }
	err := waitForContentSearchReady(context.Background(), probe, 15*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("waitForContentSearchReady() = nil, want timeout naming the last probe error")
	}
	if !strings.Contains(err.Error(), "content substring indexes are not ready") {
		t.Fatalf("timeout error = %q, want it to name the last probe error", err.Error())
	}
}

// TestStoryContentRowsAreDeterministicAndShaped pins the story corpus the
// #7009 pilots read: byte-identical plans across builds, the anchor and
// related counts, and one file of every extractor-relevant kind the stages
// must meet (module .tf, conventional compose name, kustomization base,
// workflow path).
func TestStoryContentRowsAreDeterministicAndShaped(t *testing.T) {
	t.Parallel()

	first, second := BuildStoryContentRows(), BuildStoryContentRows()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("story content plan is not deterministic")
	}
	byRepo := map[string]int{}
	kinds := map[string]bool{}
	for _, row := range first {
		byRepo[row.RepoID]++
		base := row.RelativePath[strings.LastIndex(row.RelativePath, "/")+1:]
		switch {
		case strings.HasSuffix(base, ".tf"):
			kinds["tf"] = true
		case base == "docker-compose.yml":
			kinds["compose"] = true
		case base == "kustomization.yaml":
			kinds["kustomization"] = true
		case strings.HasPrefix(row.RelativePath, ".github/workflows/"):
			kinds["workflow"] = true
		}
		if row.Content == "" {
			t.Fatalf("row %s/%s has no content", row.RepoID, row.RelativePath)
		}
	}
	if got, want := byRepo[SeedStoryAnchorID], 620; got != want {
		t.Errorf("anchor files = %d, want %d", got, want)
	}
	for i := 0; i < SeedStoryRelatedCount; i++ {
		if got, want := byRepo[SeedStoryRelatedID(i)], 120; got != want {
			t.Errorf("related %d files = %d, want %d", i, got, want)
		}
	}
	if got, want := len(first), 620+SeedStoryRelatedCount*120; got != want {
		t.Errorf("story rows = %d, want %d", got, want)
	}
	for _, kind := range []string{"tf", "compose", "kustomization", "workflow"} {
		if !kinds[kind] {
			t.Errorf("story corpus has no %s file", kind)
		}
	}
	seen := map[string]bool{}
	for _, row := range first {
		key := row.RepoID + "\x00" + row.RelativePath
		if seen[key] {
			t.Fatalf("duplicate content_files key %q", key)
		}
		seen[key] = true
	}
}

// TestSearchContentRowsCarryBothTokens pins the file-search corpus: 6,000
// documents, the common token in about one file in three of every
// repository, and the rare token in exactly three files of repository zero.
func TestSearchContentRowsCarryBothTokens(t *testing.T) {
	t.Parallel()

	rows := BuildSearchContentRows()
	if got, want := len(rows), SeedSearchRepoCount*SeedSearchFilesPerRepo; got != want {
		t.Fatalf("search rows = %d, want %d", got, want)
	}
	commonByRepo := map[string]int{}
	rare := 0
	for _, row := range rows {
		if strings.Contains(row.Content, SeedSearchFileToken) {
			commonByRepo[row.RepoID]++
		}
		if strings.Contains(row.Content, SeedSearchRareToken) {
			rare++
			if row.RepoID != SeedSearchRepoID(0) {
				t.Errorf("rare token outside repository zero: %s", row.RepoID)
			}
		}
	}
	if rare != 3 {
		t.Errorf("rare token files = %d, want 3", rare)
	}
	for r := 0; r < SeedSearchRepoCount; r++ {
		got := commonByRepo[SeedSearchRepoID(r)]
		if got < 100 || got > 200 {
			t.Errorf("repo %d common files = %d, want 100..200 of %d", r, got, SeedSearchFilesPerRepo)
		}
	}
}

// TestGateEntityRowsAllCarryTheToken pins the entity-search corpus: 300 rows
// whose source_cache all carry the entity token.
func TestGateEntityRowsAllCarryTheToken(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	rows := gateEntityCopyRows(now)
	if len(rows) != SeedEntityRowCount {
		t.Fatalf("entity rows = %d, want %d", len(rows), SeedEntityRowCount)
	}
	for i, row := range rows {
		source, _ := row[7].(string)
		if !strings.Contains(source, SeedEntityToken) {
			t.Errorf("entity row %d source_cache carries no token", i)
		}
		if row[3] != SeedEntityType {
			t.Errorf("entity row %d type = %v, want %s", i, row[3], SeedEntityType)
		}
	}
}

// TestGateEntitiesFitFirstOrderedProbe guards the entity pilot's seeded
// first window against the ordinary infra and IaC content corpus.
func TestGateEntitiesFitFirstOrderedProbe(t *testing.T) {
	t.Parallel()

	type probeRow struct {
		repo, path, id string
		line           int
		gate           bool
	}
	const probeRows = 1000
	rows := make([]probeRow, 0, len(contentDerivedInfraLabels())*(probeRows+1)+SeedEntityRowCount+len(iacEntityTypes))
	ordinaryRepos := map[string]bool{}
	for _, label := range contentDerivedInfraLabels() {
		for _, row := range infraContentEntityRows(label, idRange{First: 0, Last: probeRows}) {
			ordinaryRepos[row.RepoID] = true
			rows = append(rows, probeRow{row.RepoID, row.RelativePath, row.EntityID, 1, false})
		}
	}
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: len(scope.AllCollectorKinds())})
	if len(plan.Scopes) == 0 {
		t.Fatal("seed plan has no scopes")
	}
	for _, seededScope := range plan.Scopes {
		ordinaryRepos[seededScope.ScopeID] = true
	}
	for _, row := range BuildStoryContentRows() {
		ordinaryRepos[row.RepoID] = true
	}
	anchor := plan.Scopes[0]
	for _, row := range iacContentEntityRows(BuildIaCFacts(anchor.ScopeID, anchor.ActiveGenerationID, len(iacEntityTypes))) {
		ordinaryRepos[row.RepoID] = true
		rows = append(rows, probeRow{row.RepoID, row.RelativePath, row.EntityID, 1, false})
	}
	for _, row := range gateEntityCopyRows(time.Unix(1, 0).UTC()) {
		if ordinaryRepos[row[1].(string)] {
			t.Fatalf("gate repository %q collides with ordinary corpus", row[1])
		}
		rows = append(rows, probeRow{row[1].(string), row[2].(string), row[0].(string), row[5].(int), true})
	}
	files := BuildSearchContentRows()
	if len(files) != SeedSearchRepoCount*SeedSearchFilesPerRepo {
		t.Fatalf("search files = %d, want %d", len(files), SeedSearchRepoCount*SeedSearchFilesPerRepo)
	}
	for _, row := range files {
		if ordinaryRepos[row.RepoID] {
			t.Fatalf("file repository %q collides with ordinary corpus", row.RepoID)
		}
	}
	if got := ExpectedContentFileCounts()[strings.TrimSuffix(SeedSearchRepoID(0), "00")]; got != len(files) {
		t.Fatalf("verified search file count = %d, want %d", got, len(files))
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.repo != b.repo {
			return a.repo < b.repo
		}
		if a.path != b.path {
			return a.path < b.path
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return a.id < b.id
	})
	gateRows := 0
	for _, row := range rows[:probeRows] {
		if row.gate {
			gateRows++
		}
	}
	if gateRows != SeedEntityRowCount {
		t.Fatalf("gate rows in first ordered %d-row probe = %d, want %d", probeRows, gateRows, SeedEntityRowCount)
	}
}

// TestContentFileCopyRowsShapeOneRow pins the CopyFrom shaping: hash,
// line count and timestamp of a known row.
func TestContentFileCopyRowsShapeOneRow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	rows := contentFileCopyRows([]ContentFileSeedRow{{
		RepoID: "r", RelativePath: "infra/m.tf",
		Content: "module \"m\" {\n}\n", Language: "hcl", ArtifactType: "source",
	}}, now)
	if len(rows) != 1 || len(rows[0]) != len(contentFileColumns) {
		t.Fatalf("shaped row = %v, want %d columns", rows, len(contentFileColumns))
	}
	// Two newlines with a trailing newline is two lines under the
	// production writer rule, not three.
	if rows[0][5] != 2 {
		t.Errorf("line_count = %v, want 2", rows[0][5])
	}
	if rows[0][8] != now {
		t.Errorf("indexed_at = %v, want %v", rows[0][8], now)
	}
}

// TestGateStepFirstOnDisposablePostgres exercises the shipped StepFirst SQL
// against actual producer COPY rows on an opt-in PostgreSQL 18 fixture.
func TestGateStepFirstOnDisposablePostgres(t *testing.T) {
	dsn := os.Getenv("READ_API_PROBE_TEST_DSN")
	if dsn == "" {
		t.Skip("set READ_API_PROBE_TEST_DSN for an isolated PostgreSQL fixture")
	}
	if os.Getenv("READ_API_PROBE_TEST_DISPOSABLE") != "1" {
		t.Fatal("set READ_API_PROBE_TEST_DISPOSABLE=1 for an isolated fixture")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Host != "127.0.0.1" || config.ConnConfig.Database != "eshu_8682_probe" {
		t.Fatal("refuse nonlocal or nonfixture PostgreSQL target")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version, collation string
	if err := pool.QueryRow(ctx, "SELECT current_setting('server_version'), datcollate FROM pg_database WHERE datname = current_database()").Scan(&version, &collation); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "18.") {
		t.Fatalf("PostgreSQL version %q, want 18.x gate image", version)
	}
	var prefixBefore bool
	if err := pool.QueryRow(ctx, "SELECT 'seed-gate-search-'::text < 'seed-infra-repo-'::text").Scan(&prefixBefore); err != nil {
		t.Fatal(err)
	}
	if !prefixBefore {
		t.Fatalf("gate prefix does not sort before infra prefix under %s", collation)
	}
	t.Logf("PostgreSQL version=%s collation=%s", version, collation)
	_, err = pool.Exec(ctx, `CREATE TABLE content_entities (
      entity_id text PRIMARY KEY, repo_id text NOT NULL, relative_path text NOT NULL,
      entity_type text NOT NULL, entity_name text NOT NULL, start_line integer NOT NULL,
      end_line integer NOT NULL, language text, source_cache text NOT NULL,
      metadata jsonb NOT NULL DEFAULT '{}'::jsonb, indexed_at timestamptz NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DROP TABLE content_entities"); err != nil {
			t.Errorf("clean disposable proof table: %v", err)
		}
	}()
	_, err = pool.Exec(ctx, `CREATE INDEX content_entities_repo_path_start_idx
      ON content_entities (repo_id, relative_path, start_line, entity_id)`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1, 0).UTC()
	ordinary := make([][]any, 0, 6010)
	for _, label := range contentDerivedInfraLabels() {
		for _, row := range infraContentEntityRows(label, idRange{First: 0, Last: 1000}) {
			v, err := contentEntityCopyRow(row, now)
			if err != nil {
				t.Fatal(err)
			}
			ordinary = append(ordinary, v)
		}
	}
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: len(scope.AllCollectorKinds())})
	anchor := plan.Scopes[0]
	for _, row := range iacContentEntityRows(BuildIaCFacts(anchor.ScopeID, anchor.ActiveGenerationID, len(iacEntityTypes))) {
		v, err := contentEntityCopyRow(row, now)
		if err != nil {
			t.Fatal(err)
		}
		ordinary = append(ordinary, v)
	}
	if _, err := pool.CopyFrom(ctx, pgx.Identifier{"content_entities"}, contentEntityColumns, pgx.CopyFromRows(ordinary)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CopyFrom(ctx, pgx.Identifier{"content_entities"}, gateEntityColumns, pgx.CopyFromRows(gateEntityCopyRows(now))); err != nil {
		t.Fatal(err)
	}
	var total, gates int
	if err := pool.QueryRow(ctx, "SELECT count(*), count(*) FILTER (WHERE entity_type = $1) FROM content_entities", SeedEntityType).Scan(&total, &gates); err != nil {
		t.Fatal(err)
	}
	if total != len(ordinary)+SeedEntityRowCount || gates != SeedEntityRowCount {
		t.Fatalf("seed counts total=%d gates=%d", total, gates)
	}
	var expectedEdgeID string
	if err := pool.QueryRow(ctx, `SELECT entity_id FROM content_entities
      ORDER BY repo_id, relative_path, start_line, entity_id
      OFFSET 999 LIMIT 1`).Scan(&expectedEdgeID); err != nil {
		t.Fatal(err)
	}
	// Searcher asks for offset + limit + 1 matches to prove truncation.
	rows, err := pool.Query(ctx, unscoped.EntityStatements().StepFirst, SeedEntityToken, int64(1000), int64(11))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	hits, edges := 0, 0
	for rows.Next() {
		var kind, id, repo, path, typ, name, language, source string
		var start, end int
		var metadata []byte
		if err := rows.Scan(&kind, &id, &repo, &path, &typ, &name, &start, &end, &language, &source, &metadata); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "hit":
			if typ != SeedEntityType || !strings.Contains(source, SeedEntityToken) {
				t.Fatalf("wrong hit id=%s repo=%s type=%s", id, repo, typ)
			}
			wantID := fmt.Sprintf("gate-entity-%04d", hits*3)
			if id != wantID {
				t.Fatalf("hit[%d] id=%s want=%s", hits, id, wantID)
			}
			hits++
		case "edge":
			if id != expectedEdgeID || repo == "" || path == "" || start != 1 || source != "" {
				t.Fatalf("invalid edge id=%s repo=%s path=%s start=%d", id, repo, path, start)
			}
			edges++
		default:
			t.Fatalf("unexpected row kind %s", kind)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("StepFirst hits=%d edges=%d total=%d", hits, edges, total)
	if hits != 11 || edges != 1 {
		t.Fatalf("StepFirst hits=%d edges=%d, want 11 and 1", hits, edges)
	}
}
