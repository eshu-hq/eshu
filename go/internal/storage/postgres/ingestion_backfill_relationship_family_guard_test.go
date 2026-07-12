// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestDeferredRelationshipFamilyGuardWrapsPayloadScanningArms(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listDeferredScopedRelationshipFactRecordsQuery, deferredRelationshipFamilyPayloadFactsFilterSQL) {
		t.Fatalf(
			"deferred scoped relationship query does not build the payload-scanning arm from relationship-family facts",
		)
	}

	for _, want := range []string{
		"relationship_family_payload_facts AS MATERIALIZED",
		"lower(fact.payload::text) AS payload_lower",
		"FROM relationship_family_payload_facts AS fact\n        WHERE fact.payload_lower LIKE ANY($1)",
	} {
		if !strings.Contains(listDeferredScopedRelationshipFactRecordsQuery, want) {
			t.Fatalf("deferred scoped relationship query lost guarded payload arm fragment %q", want)
		}
	}

	forbiddenUnguardedPayloadScan := "FROM source_facts AS fact\n        WHERE fact.payload_lower"
	if strings.Contains(listDeferredScopedRelationshipFactRecordsQuery, forbiddenUnguardedPayloadScan) {
		t.Fatalf("deferred scoped relationship query still scans payload_lower directly from source_facts")
	}

	for _, want := range []string{
		"relationship_reference_candidate_keys AS ref",
		"position('|' || catalog_repo_id.reference_key || '|' in ref.reference_key) > 0",
		"ref.scope_id = $3",
		"FROM relationship_family_payload_facts AS fact\n        WHERE NOT EXISTS (",
		"fact.own_repo_id = $6 AND $5::text IS NOT NULL AND fact.payload_lower ~ $5",
	} {
		if !strings.Contains(listDeferredScopedRelationshipFactRecordsQuery, want) {
			t.Fatalf("deferred scoped relationship query lost required repo-id arm fragment %q", want)
		}
	}
}

func TestDeferredRelationshipFamilyGuardKeepsExtractorFamilies(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"'gcp_cloud_relationship'",
		"'terraform'",
		"'terraform_hcl'",
		"'terraform_template_text'",
		"'terragrunt'",
		"'helm'",
		"'argocd'",
		"'dockerfile'",
		"'docker_compose'",
		"'github_actions_workflow'",
		"LIKE 'ansible_%'",
		"\\.(tf|tf\\.json|tfvars|tfvars\\.json|hcl|tpl)",
		"chart\\.ya?ml",
		"kustomization\\.ya?ml",
		"docker-compose\\.ya?ml",
		"\\.github/workflows",
		"(playbooks|roles|group_vars|host_vars|inventories)",
	} {
		if !strings.Contains(deferredRelationshipFamilyCandidatePredicateSQL, want) {
			t.Fatalf("relationship-family predicate missing extractor-family fragment %q", want)
		}
	}
}

func TestDeferredRelationshipFamilyGuardKeepsDockerfileAndJenkinsfilePrefixes(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"dockerfile(\\.[^/]*)?",
		"jenkinsfile(\\.[^/]*)?",
	} {
		if !strings.Contains(deferredRelationshipFamilyCandidatePredicateSQL, want) {
			t.Fatalf("relationship-family predicate missing prefix basename fragment %q", want)
		}
	}
}

func TestDeferredRelationshipFamilyGuardKeepsArgoCDCarveOuts(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"argocd/.*\\.ya?ml",
		"applicationsets?/.*\\.ya?ml",
		"CASE",
		"\\.ya?ml$",
		"kind: application",
		"kind: applicationset",
		"ELSE false",
	} {
		if !strings.Contains(deferredRelationshipFamilyCandidatePredicateSQL, want) {
			t.Fatalf("relationship-family predicate missing ArgoCD/ApplicationSet carve-out %q", want)
		}
	}
}

func TestDeferredRelationshipFamilyGuardKeepsSaltGitfsFallback(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"gitfs_remotes",
		"(^|\\n)gitfs_remotes[[:space:]]*:",
	} {
		if !strings.Contains(deferredRelationshipFamilyCandidatePredicateSQL, want) {
			t.Fatalf("relationship-family predicate missing Salt gitfs fallback fragment %q", want)
		}
	}
}

func TestDeferredRelationshipFamilyGuardPathGatesSaltGitfsFallback(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		deferredRelationshipFamilyPathSQL + ` ~ `,
		"~ '\\.ya?ml$'",
		"ya?ml$",
		"AND COALESCE",
	} {
		if !strings.Contains(deferredRelationshipFamilySaltGitfsContentMarkerSQL, want) {
			t.Fatalf("Salt gitfs fallback is not path-gated before content scan; missing %q", want)
		}
	}
	if strings.Contains(deferredRelationshipFamilySaltGitfsContentMarkerSQL, "~ '\\\\.ya?ml$'") {
		t.Fatal("Salt gitfs fallback escaped the YAML extension regex as a literal backslash path")
	}
}

func TestDeferredRelationshipFamilyGuardScopeFencesReferenceKeyArms(t *testing.T) {
	t.Parallel()

	if count := strings.Count(listDeferredScopedRelationshipFactRecordsQuery, "AND ref.scope_id = $3"); count != 2 {
		t.Fatalf("reference-key arms should both fence ref rows by scope; got %d matching fences", count)
	}
	if count := strings.Count(listDeferredScopedRelationshipFactRecordsQuery, "AND ref.generation_id = $4"); count != 2 {
		t.Fatalf("reference-key arms should both fence ref rows by generation; got %d matching fences", count)
	}
}

func TestDeferredRelationshipFamilyGuardDoesNotAdmitGenericContentFamilies(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{
		"'php'",
		"pipfile",
		"package.json",
		"robots",
		"composer",
	} {
		if strings.Contains(strings.ToLower(deferredRelationshipFamilyCandidatePredicateSQL), forbidden) {
			t.Fatalf("relationship-family predicate admits generic content family %q", forbidden)
		}
	}
}

func TestDeferredRelationshipFamilyGuardRetainedDBEvidenceEquivalence(t *testing.T) {
	dsn := dsnForRelationshipFamilyGuardProof(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	db := openDeferredHoistProofDB(t, dsn)
	catalog, _, err := loadRepositoryCatalog(ctx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("load repository catalog: %v", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("expected retained DB catalog to produce usable deferred fact-load params")
	}

	for _, scopeID := range relationshipFamilyGuardProofScopes() {
		generationID := activeGenerationForProofScope(t, ctx, db, scopeID)

		startOld := time.Now()
		oldLoad := runPreRelationshipFamilyGuardDeferredScopedQuery(
			t, ctx, db, params, scopeID, generationID,
		)
		oldDuration := time.Since(startOld)

		startNew := time.Now()
		newLoad := runHoistedDeferredScopedQuery(t, ctx, db, params, scopeID, generationID)
		newDuration := time.Since(startNew)

		oldEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(oldLoad, catalog))
		newEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(newLoad, catalog))
		if !evidenceSetsEqual(oldEvidence, newEvidence) {
			t.Fatalf(
				"relationship-family guard evidence diverged for scope %s generation %s\nold_facts=%d new_facts=%d old_evidence=%d new_evidence=%d",
				scopeID,
				generationID,
				len(oldLoad),
				len(newLoad),
				len(oldEvidence),
				len(newEvidence),
			)
		}
		if len(newLoad) > len(oldLoad) {
			t.Fatalf("relationship-family guard increased loaded facts for scope %s: old=%d new=%d", scopeID, len(oldLoad), len(newLoad))
		}
		if scopeID == "git-repository-scope:repository:r_de3355a0" && newDuration >= oldDuration {
			t.Fatalf(
				"relationship-family guard preserved task777 evidence but did not improve load time: old=%s new=%s",
				oldDuration,
				newDuration,
			)
		}
		t.Logf(
			"relationship-family guard proof scope=%s generation=%s old_facts=%d new_facts=%d old_evidence=%d new_evidence=%d old_load=%s new_load=%s",
			scopeID,
			generationID,
			len(oldLoad),
			len(newLoad),
			len(oldEvidence),
			len(newEvidence),
			oldDuration,
			newDuration,
		)
	}
}

func TestDeferredRelationshipFamilyGuardRetainedDBWorstPartitionLoad(t *testing.T) {
	dsn := dsnForRelationshipFamilyGuardProof(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db := openDeferredHoistProofDB(t, dsn)
	catalog, _, err := loadRepositoryCatalog(ctx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("load repository catalog: %v", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("expected retained DB catalog to produce usable deferred fact-load params")
	}

	const task777Scope = "git-repository-scope:repository:r_de3355a0"
	generationID := activeGenerationForProofScope(t, ctx, db, task777Scope)
	start := time.Now()
	loaded := runHoistedDeferredScopedQuery(t, ctx, db, params, task777Scope, generationID)
	duration := time.Since(start)
	if len(loaded) != 0 {
		t.Fatalf("relationship-family guard loaded %d facts for task777, want 0", len(loaded))
	}
	if duration >= 30*time.Second {
		t.Fatalf("relationship-family guard task777 load took %s, want <30s", duration)
	}
	t.Logf(
		"relationship-family guard task777 load proof scope=%s generation=%s new_facts=%d new_load=%s",
		task777Scope,
		generationID,
		len(loaded),
		duration,
	)
}

func dsnForRelationshipFamilyGuardProof(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("ESHU_RELATIONSHIP_FAMILY_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_LATEST_GENERATION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	t.Skip("set ESHU_RELATIONSHIP_FAMILY_PROOF_DSN (or ESHU_DEFERRED_PARTITION_PROOF_DSN / ESHU_LATEST_GENERATION_PROOF_DSN) to run the retained DB relationship-family guard proof")
	return ""
}

func relationshipFamilyGuardProofScopes() []string {
	raw := os.Getenv("ESHU_RELATIONSHIP_FAMILY_PROOF_SCOPES")
	if raw == "" {
		return []string{
			"git-repository-scope:repository:r_de3355a0",
			"git-repository-scope:repository:r_d393ab02",
			"git-repository-scope:repository:r_415c2ca3",
			"git-repository-scope:repository:r_d737ae8e",
		}
	}

	parts := strings.Split(raw, ",")
	scopes := make([]string, 0, len(parts))
	for _, part := range parts {
		scope := strings.TrimSpace(part)
		if scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func activeGenerationForProofScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
) string {
	t.Helper()

	var generation sql.NullString
	if err := db.QueryRowContext(
		ctx,
		"SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1",
		scopeID,
	).Scan(&generation); err != nil {
		t.Fatalf("load active generation for scope %s: %v", scopeID, err)
	}
	if !generation.Valid || strings.TrimSpace(generation.String) == "" {
		t.Fatalf("scope %s has no active_generation_id", scopeID)
	}
	return generation.String
}

func runPreRelationshipFamilyGuardDeferredScopedQuery(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	params deferredScopedFactQueryParams,
	scopeID, generationID string,
) []facts.Envelope {
	t.Helper()

	preGuardQuery := strings.Replace(
		listDeferredScopedRelationshipFactRecordsQuery,
		deferredRelationshipFamilyPayloadFactsFilterSQL,
		"",
		1,
	)
	if preGuardQuery == listDeferredScopedRelationshipFactRecordsQuery {
		t.Fatal("failed to derive pre-relationship-family-guard query from production query")
	}

	ownRepoID := deferredScopedFactOwnRepoIDFromScope(scopeID)
	regex, ok := buildDeferredRepoIDRegex([]string(params.repoIDValues), ownRepoID)
	repoIDReferenceKeys := deferredRepoIDReferenceKeys(params.repoIDValues, params.repoIDReferenceKey)
	var regexParam sql.NullString
	if ok {
		regexParam = sql.NullString{String: regex, Valid: true}
	}

	rows, err := db.QueryContext(
		ctx,
		preGuardQuery,
		params.nonRepoIDLike,
		params.repoIDValues,
		scopeID,
		generationID,
		regexParam,
		ownRepoID,
		repoIDReferenceKeys,
	)
	if err != nil {
		t.Fatalf("query pre-relationship-family guard: %v", err)
	}
	return collectDeferredScopedRows(t, rows)
}
