// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8833 disposable PostgreSQL proof of the operator cleanup on docs/public/deployment/credential-file-cleanup.md, run from the page's SQL fences: the census counts every pre-watermark raw copy (files, entities, references, active search documents and fact payloads, orphans, a NULL-generation row), statement L lists the repositories, one guarded pass zeroes every exit column and the raw-rows gauge, a second pass touches nothing, post-watermark rows, superseded generations, and non-matching paths survive, and the production active-document read returns no seeded context text; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

const cleanupSeed = "CleanupSeed0123456789"

var (
	cleanupPolicyPaths = []string{".yarnrc.yml", "app/.yarnrc.yml", "prod.tfvars", "a/b/prod.auto.tfvars", "envs/x.tfvars.json"}
	cleanupOtherPaths  = []string{"my.yarnrc.yml", "x.yarnrc.yml.bak", "tfvars", "README.md", "main.tf"}
)

type cleanupFixture struct {
	t        *testing.T
	ctx      context.Context
	db       *sql.DB
	w        time.Time
	writer   ContentWriter
	scopeSeq int
}

func (f *cleanupFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatalf("exec %.80q: %v", query, err)
	}
}

// scope seeds a repository scope with one active generation ingested at the
// given time and returns its scope and generation ids.
func (f *cleanupFixture) scope(repo string, ingested time.Time) (string, string) {
	f.scopeSeq++
	scopeID := fmt.Sprintf("scope-%s-%d", repo, f.scopeSeq)
	generation := scopeID + "-gen"
	f.exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)`, scopeID, ingested)
	f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $3, 'active')`, generation, scopeID, ingested)
	f.exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generation, scopeID)
	return scopeID, generation
}

// content writes the paths through the production content writer, with an
// entity for each, so references and secret lines derive as they do live.
func (f *cleanupFixture) content(repo, scopeID, generation string, paths []string, body string) {
	f.t.Helper()
	materialization := content.Materialization{RepoID: repo, ScopeID: scopeID, GenerationID: generation}
	for i, path := range paths {
		materialization.Records = append(materialization.Records, content.Record{Path: path, Body: body})
		materialization.Entities = append(materialization.Entities, content.EntityRecord{
			EntityID: fmt.Sprintf("entity-%s-%s-%d", repo, generation, i), Path: path, EntityType: "Variable",
			EntityName: "v", StartLine: 1, EndLine: 2, SourceCache: body,
		})
	}
	if _, err := f.writer.Write(f.ctx, materialization); err != nil {
		f.t.Fatalf("write content: %v", err)
	}
}

// searchCopies seeds an index document, a term, and a reducer fact for a path
// in one generation. withFact=false leaves an orphan document, withDoc=false an
// orphan fact.
func (f *cleanupFixture) searchCopies(repo, scopeID, generation, path string, withDoc, withFact bool) {
	documentID := "searchdoc:content_file:" + repo + ":" + path
	factID := "fact-" + generation + "-" + path
	if withDoc {
		f.exec(`INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
    source_kind, document, document_length, updated_at)
VALUES ($1, $2, $3, $4, $5, 'content_file', jsonb_build_object('Path', $6::text, 'ContextText', 'token: ' || $7::text), 2, now())`,
			scopeID, generation, documentID, factID, repo, path, cleanupSeed)
		f.exec(`INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, $3, 'tk', lower($4::text), 1)`, scopeID, generation, documentID, cleanupSeed)
	}
	if withFact {
		f.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'reducer_eshu_search_document', $1, 'git', $1, now(), now(),
    jsonb_build_object('repo_id', $4::text, 'document', jsonb_build_object('Path', $5::text, 'ContextText', 'token: ' || $6::text)))`,
			factID, scopeID, generation, repo, path, cleanupSeed)
	}
}

func (f *cleanupFixture) count(query string, args ...any) int64 {
	f.t.Helper()
	var n int64
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		f.t.Fatalf("count %.80q: %v", query, err)
	}
	return n
}

var cleanupExitColumns = []string{
	"yarnrc_yml.raw_files", "tfvars.raw_files", "yarnrc_yml.files_with_raw_finding", "tfvars.files_with_raw_finding",
	"raw_entity_rows", "raw_reference_rows", "raw_search_documents", "raw_search_fact_payloads",
}

// census runs the page's census 2 and 3 and returns the columns by name.
func (f *cleanupFixture) census(census map[string]string) map[string]int64 {
	f.t.Helper()
	out := map[string]int64{}
	for _, name := range []string{"2", "3"} {
		for column, value := range queryRowsMap(f.t, f.ctx, f.db, census[name]) {
			if value.Valid {
				var n int64
				if _, err := fmt.Sscan(value.String, &n); err == nil {
					out[column] = n
				}
			}
		}
	}
	return out
}

func TestCredentialCleanupPurgeFromThePageLive(t *testing.T) {
	proof := openReprojectProof(t)
	f := &cleanupFixture{t: t, ctx: t.Context(), db: proof.db, w: time.Now().UTC().Truncate(time.Second), writer: NewContentWriter(SQLDB{DB: proof.db})}
	censusFence, purgeFence := credentialCleanupFences(t)
	census := splitSections(t, censusFence, censusHeadPattern)
	purge := splitSections(t, purgeFence, statementHeadPattern)
	raw := "npmAuthToken: " + cleanupSeed + "\npassword = \"" + cleanupSeed + "\"\nnpmRegistryServer: \"https://registry.internal.acme.io/\"\n"

	// r1: everything raw, written before the watermark.
	r1Scope, r1Gen := f.scope("r1", f.w.Add(-time.Hour))
	f.content("r1", r1Scope, r1Gen, append(append([]string{}, cleanupPolicyPaths...), cleanupOtherPaths...), raw)
	f.content("r1", r1Scope, r1Gen, []string{"nullgen.tfvars"}, raw)
	f.exec(`UPDATE content_files SET generation_id = NULL WHERE repo_id = 'r1' AND relative_path = 'nullgen.tfvars'`)
	f.exec(`INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line,
    language, source_cache, indexed_at) VALUES ('ghost', 'r1', 'ghost.tfvars', 'Variable', 'v', 1, 2, '', $1, now())`, raw)
	f.searchCopies("r1", r1Scope, r1Gen, ".yarnrc.yml", true, true)
	f.searchCopies("r1", r1Scope, r1Gen, "prod.tfvars", true, true)
	f.searchCopies("r1", r1Scope, r1Gen, "main.tf", true, true)
	f.searchCopies("r1", r1Scope, r1Gen, "app/.yarnrc.yml", true, false)
	f.searchCopies("r1", r1Scope, r1Gen, "a/b/prod.auto.tfvars", false, true)
	f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ($1, $2, 'snapshot', $3, $3, 'superseded', $3)`, r1Scope+"-old", r1Scope, f.w.Add(-48*time.Hour))
	f.searchCopies("r1", r1Scope, r1Scope+"-old", ".yarnrc.yml", true, true)
	// r2: rebuilt after the watermark (redacted body); the purge must not touch it.
	r2Scope, r2Gen := f.scope("r2", f.w.Add(time.Hour))
	f.content("r2", r2Scope, r2Gen, []string{".yarnrc.yml"}, "npmAuthToken: REDACTED_api_token\n")
	f.searchCopies("r2", r2Scope, r2Gen, ".yarnrc.yml", true, true)
	f.exec(`INSERT INTO runtime_ingester_control (ingester, reindex_request_status, reindex_request_requested_at)
VALUES ('repository', 'requested', $1)`, f.w)

	vars := map[string]string{"pred": credentialCleanupPred, "w": f.w.Format(time.RFC3339)}
	before := f.census(census)
	for _, column := range cleanupExitColumns {
		if before[column] == 0 {
			t.Errorf("census before the purge: %s = 0, want raw copies (the proof would pass on an empty database)", column)
		}
	}
	gauge := NewContentPolicyRawRowsStore(SQLDB{DB: f.db}, parser.DefaultRegistry().ContentStorePathPattern())
	if n, err := gauge.ContentPolicyRawRows(f.ctx); err != nil || n == 0 {
		t.Fatalf("raw-rows gauge before = %d (%v), want raw rows", n, err)
	}

	rows, err := f.db.QueryContext(f.ctx, bindPSQL(t, purge["L"], vars))
	if err != nil {
		t.Fatalf("statement L: %v", err)
	}
	var repos []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			t.Fatalf("scan L: %v", err)
		}
		repos = append(repos, repo)
	}
	_ = rows.Close()
	if strings.Join(repos, ",") != "r1,r2" && strings.Join(repos, ",") != "r2,r1" {
		t.Fatalf("statement L = %v, want r1 and r2", repos)
	}

	pass := func() map[string]int64 {
		touched := map[string]int64{}
		for _, repo := range repos {
			vars["repo"] = repo
			for _, name := range []string{"refs", "entities", "files", "A", "B"} {
				for i := 0; ; i++ {
					n := runScript(t, f.ctx, f.db, bindPSQL(t, purge[name], vars))
					touched[name] += n
					if n == 0 || i > 20 {
						break
					}
				}
			}
		}
		return touched
	}
	first := pass()
	for _, name := range []string{"refs", "entities", "files", "A", "B"} {
		if first[name] == 0 {
			t.Errorf("first pass: statement %s touched 0 rows, want the raw copies", name)
		}
	}
	after := f.census(census)
	for _, column := range cleanupExitColumns {
		if after[column] != 0 {
			t.Errorf("census after the purge: %s = %d, want 0", column, after[column])
		}
	}
	if n, err := gauge.ContentPolicyRawRows(f.ctx); err != nil || n != 0 {
		t.Errorf("raw-rows gauge after = %d (%v), want 0", n, err)
	}
	if second := pass(); len(second) != 5 || second["refs"]+second["entities"]+second["files"]+second["A"]+second["B"] != 0 {
		t.Errorf("second pass touched %v, want 0 rows everywhere", second)
	}

	// Survivors: non-matching paths, the post-watermark repository, and the
	// superseded generation's documents keep their rows and bodies.
	for _, path := range cleanupOtherPaths {
		if n := f.count(`SELECT count(*) FROM content_files WHERE repo_id = 'r1' AND relative_path = $1 AND strpos(content, $2) > 0`, path, cleanupSeed); n != 1 {
			t.Errorf("non-matching %s lost its body", path)
		}
	}
	if n := f.count(`SELECT count(*) FROM content_files WHERE repo_id = 'r2' AND content <> ''`); n != 1 {
		t.Errorf("post-watermark r2 file was blanked")
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE generation_id = $1`, r2Gen); n != 1 {
		t.Errorf("post-watermark r2 document was deleted")
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE generation_id = $1`, r1Scope+"-old"); n != 1 {
		t.Errorf("superseded generation's document was deleted; reads never serve it and retention owns it")
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE generation_id = $1 AND document->>'Path' = 'main.tf'`, r1Gen); n != 1 {
		t.Errorf("non-matching main.tf document was deleted")
	}
	if n := f.count(`SELECT count(*) FROM content_entities WHERE entity_id = 'ghost' AND source_cache = ''`); n != 1 {
		t.Errorf("the entity with no file row kept its source_cache")
	}
	documents, err := NewEshuSearchDocumentStore(SQLDB{DB: f.db}).ListActiveDocuments(f.ctx, EshuSearchDocumentFilter{ScopeID: r1Scope})
	if err != nil {
		t.Fatalf("ListActiveDocuments: %v", err)
	}
	for _, document := range documents {
		if strings.Contains(document.Document.ContextText, cleanupSeed) && document.Document.Path != "main.tf" {
			t.Errorf("active document %s still serves the seeded context text", document.Document.Path)
		}
	}
}
