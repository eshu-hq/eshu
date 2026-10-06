// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// corpusBase anchors every timestamp the composed corpus seeds; the prepass
// runs at +1h.
var corpusBase = time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

// reopenDomains are every reducer domain the maintenance passes reopen: the
// two relationship domains and the cross-scope correlation domains.
func reopenDomains() []string {
	return append([]string{"deployment_mapping", "code_import_repo_edge"}, postgres.CrossScopeCorrelationReopenDomains()...)
}

// corpus seeds one fully bootstrapped schema. It copies the single-arm half
// of the root targeted-maintenance differential seeders
// (ingestion_targeted_maintenance_harness_test.go), which stay with the
// partition-scoped pass's own proofs in the root package.
type corpus struct {
	t   *testing.T
	ctx context.Context
	db  *sql.DB
}

func (c *corpus) exec(query string, args ...any) {
	c.t.Helper()
	if _, err := c.db.ExecContext(c.ctx, query, args...); err != nil {
		c.t.Fatalf("seed %q: %v", query, err)
	}
}

// scope seeds one ingestion scope with no active generation.
func (c *corpus) scope(scopeID string) {
	c.t.Helper()
	c.exec(`INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', NULL)`, scopeID, corpusBase)
}

// generation seeds one generation at offset and, when activate, moves the
// scope's active pointer to it and supersedes the previous active generation.
func (c *corpus) generation(scopeID, generationID string, offset time.Duration, activate bool) {
	c.t.Helper()
	status := "pending"
	if activate {
		status = "active"
		c.exec(`UPDATE scope_generations SET status = 'superseded', superseded_at = $2
WHERE scope_id = $1 AND status = 'active'`, scopeID, corpusBase.Add(offset))
	}
	c.exec(`INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'poll', $3, $3, $4, CASE WHEN $4 = 'active' THEN $3::timestamptz END)`,
		generationID, scopeID, corpusBase.Add(offset), status)
	if activate {
		c.exec(`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID)
	}
}

// fact seeds one fact row of kind with a JSON payload.
func (c *corpus) fact(factID, scopeID, generationID, kind, payload string) {
	c.t.Helper()
	c.exec(`INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, 'git', $1, $5, $5, $6::jsonb)`,
		factID, scopeID, generationID, kind, corpusBase, payload)
}

// repo seeds a repository fact for repoID named name in the partition.
func (c *corpus) repo(scopeID, generationID, repoID, name string) {
	c.t.Helper()
	c.fact("repo-"+generationID+"-"+repoID, scopeID, generationID, "repository",
		fmt.Sprintf(`{"repo_id":%q,"name":%q}`, repoID, name))
}

// terraformRef seeds a Terraform content fact in repoID naming targetAlias as
// an app_repo.
func (c *corpus) terraformRef(factID, scopeID, generationID, repoID, path, targetAlias string) {
	c.t.Helper()
	c.fact(factID, scopeID, generationID, "content", fmt.Sprintf(
		`{"repo_id":%q,"artifact_type":"terraform","relative_path":%q,"content":"app_repo = \"%s\""}`,
		repoID, path, targetAlias))
}

// gitRepo seeds a git scope with one active generation holding one repository.
func (c *corpus) gitRepo(scopeID, generationID, repoID, name string) {
	c.t.Helper()
	c.scope(scopeID)
	c.generation(scopeID, generationID, 0, true)
	c.repo(scopeID, generationID, repoID, name)
}

// workItems seeds one succeeded reducer work item per reopen domain in the
// partition, with ids "<generation>/<domain>".
func (c *corpus) workItems(scopeID, generationID string) {
	c.t.Helper()
	for _, domain := range reopenDomains() {
		c.exec(`INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, 'succeeded', 1, $5, $5)`,
			generationID+"/"+domain, scopeID, generationID, domain, corpusBase)
	}
}

// prepass runs the real corpus-wide backfill (evidence, phase and memo) at
// +1h, the steady state an obligation starts from.
func (c *corpus) prepass() {
	c.t.Helper()
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: c.db})
	store.Now = func() time.Time { return corpusBase.Add(time.Hour) }
	if err := store.BackfillAllRelationshipEvidence(c.ctx, nil, nil); err != nil {
		c.t.Fatalf("prepass BackfillAllRelationshipEvidence() error = %v", err)
	}
}
