// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRelationshipFamilyCandidateFactIDRowsKeepExtractorFamilies(t *testing.T) {
	t.Parallel()

	rows := relationshipFamilyCandidateFactIDRows([]facts.Envelope{
		familyCandidateEnvelope("terraform", "main.tf", ""),
		familyCandidateEnvelope("helm", "chart/values.yaml", ""),
		familyCandidateEnvelope("github_actions_workflow", ".github/workflows/ci.yml", ""),
		familyCandidateEnvelope("ansible_playbook", "playbooks/site.yml", ""),
		familyCandidateEnvelope("", "apps/applicationsets/prod.yaml", ""),
		familyCandidateEnvelope("", "random/deploy.yaml", "kind: ApplicationSet\nmetadata:\n  name: demo"),
		{
			FactID:       "gcp-relationship",
			ScopeID:      "gcp-scope",
			GenerationID: "gen",
			FactKind:     facts.GCPCloudRelationshipFactKind,
			ObservedAt:   time.Unix(1, 0).UTC(),
			Payload:      map[string]any{"target": "repository:r_target"},
		},
	})
	if got, want := len(rows), 7; got != want {
		t.Fatalf("relationshipFamilyCandidateFactIDRows() returned %d rows, want %d: %#v", got, want, rows)
	}
	for _, row := range rows {
		if row.FactID == "" || row.ScopeID == "" || row.GenerationID == "" {
			t.Fatalf("candidate row has empty identity: %#v", row)
		}
	}
}

func TestRelationshipFamilyCandidateFactIDRowsSkipGenericContent(t *testing.T) {
	t.Parallel()

	rows := relationshipFamilyCandidateFactIDRows([]facts.Envelope{
		familyCandidateEnvelope("php", "public/index.php", "<?php echo 'robots s3 dma';"),
		familyCandidateEnvelope("", "Pipfile", "requests = '*'"),
		{
			FactID:       "repository",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "repository",
			ObservedAt:   time.Unix(1, 0).UTC(),
			Payload:      map[string]any{"repo_id": "repository:r_demo"},
		},
		{
			FactID:       "tombstone",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "content",
			IsTombstone:  true,
			ObservedAt:   time.Unix(1, 0).UTC(),
			Payload:      map[string]any{"artifact_type": "terraform", "relative_path": "main.tf"},
		},
	})
	if len(rows) != 0 {
		t.Fatalf("relationshipFamilyCandidateFactIDRows() = %#v, want no generic rows", rows)
	}
}

func TestRefreshRelationshipFamilyCandidateFactIDsDeletesAcceptedFactIDsBeforeInsert(t *testing.T) {
	t.Parallel()

	db := &relationshipReferenceExecRecorder{}
	err := refreshRelationshipFamilyCandidateFactIDs(context.Background(), db, []facts.Envelope{
		familyCandidateEnvelope("terraform", "main.tf", ""),
		familyCandidateEnvelope("php", "public/index.php", "robots s3"),
		{
			FactID:       "tombstone",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "content",
			IsTombstone:  true,
			ObservedAt:   time.Unix(1, 0).UTC(),
			Payload:      map[string]any{"artifact_type": "terraform", "relative_path": "main.tf"},
		},
	})
	if err != nil {
		t.Fatalf("refreshRelationshipFamilyCandidateFactIDs() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM relationship_family_candidate_fact_ids") {
		t.Fatalf("first exec query = %q, want family id delete", db.execs[0].query)
	}
	deleted, ok := db.execs[0].args[0].(pq.StringArray)
	if !ok {
		t.Fatalf("delete arg type = %T, want pq.StringArray", db.execs[0].args[0])
	}
	wantDeleted := []string{"fact-main-tf", "fact-public-index-php", "tombstone"}
	for i, want := range wantDeleted {
		if i >= len(deleted) || deleted[i] != want {
			t.Fatalf("deleted fact ids = %v, want %v", []string(deleted), wantDeleted)
		}
	}
	if !strings.Contains(db.execs[1].query, "INSERT INTO relationship_family_candidate_fact_ids") {
		t.Fatalf("second exec query = %q, want family id insert", db.execs[1].query)
	}
	if got, want := len(db.execs[1].args), columnsPerRelationshipFamilyCandidateFactIDRow; got != want {
		t.Fatalf("insert args = %d, want one candidate row (%d args)", got, want)
	}
	if got, want := db.execs[1].args[0], "fact-main-tf"; got != want {
		t.Fatalf("insert fact_id = %v, want %q", got, want)
	}
}

func TestDeferredRelationshipFamilyIDQueryUsesAliasOnlySurface(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"relationship_family_candidate_fact_ids AS family",
		"family.scope_id = $3",
		"family.generation_id = $4",
		"lower(fact.payload::text) LIKE ANY($1)",
	} {
		if !strings.Contains(listDeferredScopedRelationshipFactRecordsQuery, want) {
			t.Fatalf("deferred scoped query missing family-ID alias-only fragment %q", want)
		}
	}
	for _, forbidden := range []string{
		"position('|' || catalog_repo_id.reference_key || '|' in ref.reference_key) > 0",
		"fact.own_repo_id = $6 AND $5::text IS NOT NULL AND fact.payload_lower ~ $5",
	} {
		if strings.Contains(listDeferredScopedRelationshipFactRecordsQuery, forbidden) {
			t.Fatalf("deferred scoped query still contains rejected family-scoped arm %q", forbidden)
		}
	}
}

func familyCandidateEnvelope(artifactType, path, content string) facts.Envelope {
	payload := map[string]any{
		"artifact_type": artifactType,
		"relative_path": path,
		"repo_id":       "repository:r_source",
	}
	if content != "" {
		payload["content"] = content
	}
	idPart := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(strings.ToLower(strings.Trim(path, "/")))
	if idPart == "" {
		idPart = strings.ToLower(strings.TrimSpace(artifactType))
	}
	return facts.Envelope{
		FactID:       "fact-" + strings.Trim(idPart, "-"),
		ScopeID:      "git-repository-scope:repository:r_source",
		GenerationID: "gen",
		FactKind:     "content",
		ObservedAt:   time.Unix(1, 0).UTC(),
		Payload:      payload,
	}
}
