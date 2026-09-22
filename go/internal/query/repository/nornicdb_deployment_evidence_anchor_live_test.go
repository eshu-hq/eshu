// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live accuracy proof for #6811: QueryRepoDeploymentEvidence's incoming read
// (deployment_evidence.go) and FetchFluxDeploymentSourceTargetBindings'
// expansion read (impacttrace/impact_trace_deployment_flux_bindings.go) both
// anchor at the arrow head of an incoming relationship into the bound
// repository -- the shape #6794 proved costs disproportionate time on
// NornicDB and that this backend can also return the wrong row set for (see
// docs/internal/evidence/6794-context-incoming-anchor.md and the "A Pattern
// Anchored At The Arrow Head Scans Its Tail Label" pitfall in
// docs/public/reference/nornicdb-query-pitfalls.md).
//
// ROW-SET TRUTH ONLY -- no timing here; that needs a fresh container per
// shape/scale (see the #6811 evidence note). Seeds a hub with exactly 40
// incoming EvidenceArtifacts, a leaf with zero, a mid-graph repo with
// exactly 2, all known by construction, plus ESHU_6811_FILLER (default 300)
// filler repos wired to EACH OTHER so the hub's answer stays 40 regardless
// of filler size. Runs the production function, the pre-change
// (right-anchored) statement verbatim, and a candidate bound-anchored
// statement, and asserts the CANDIDATE matches the known truth. The
// pre-change statement is compared and logged but does NOT gate the test --
// a wrong row count there is the #6811 finding this file records, not a
// reason to weaken the assertion.
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test ./internal/query/repository \
//	  -tags live_nornicdb_answer_truth -run TestLiveNornicDBDeploymentEvidenceAnchor -count=1 -v -timeout 20m
package repository

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	storagecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	deployEvidenceAnchorPrefix        = "deploy-evidence-anchor:"
	deployEvidenceAnchorCleanup       = `MATCH (n) WHERE n.id STARTS WITH '` + deployEvidenceAnchorPrefix + `' DETACH DELETE n`
	deployEvidenceAnchorHubIncoming   = 40
	deployEvidenceAnchorMidIncoming   = 2
	deployEvidenceAnchorFillerEnv     = "ESHU_6811_FILLER"
	deployEvidenceAnchorDefaultFiller = 300
	deployEvidenceAnchorWriteBatch    = 500
)

// Pre-change (right-anchored) statements, kept verbatim as the parity oracle.
// deployEvidenceAnchorOldIncoming mirrors QueryRepoDeploymentEvidence's
// incoming branch exactly (deployment_evidence.go); deployEvidenceAnchorOldFlux
// mirrors FetchFluxDeploymentSourceTargetBindings' expansion read exactly
// (impact_trace_deployment_flux_bindings.go), with the unscoped
// (AllScopes:true) access predicates -- which are both empty strings --
// already inlined.
const (
	deployEvidenceAnchorOldIncoming = `
		MATCH (artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository {id: $repo_id})
		WITH artifact, r
		MATCH (source:Repository)-[:HAS_DEPLOYMENT_EVIDENCE]->(artifact)
		RETURN 'incoming' AS direction,
		       artifact.id AS artifact_id,
		       artifact.path AS path,
		       source.id AS source_repo_id,
		       r.id AS target_repo_id
		ORDER BY path, artifact_id
		LIMIT $limit
	`
	deployEvidenceAnchorCandidateIncoming = `
		MATCH (r:Repository {id: $repo_id})<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(artifact:EvidenceArtifact)<-[:HAS_DEPLOYMENT_EVIDENCE]-(source:Repository)
		RETURN 'incoming' AS direction,
		       artifact.id AS artifact_id,
		       artifact.path AS path,
		       source.id AS source_repo_id,
		       r.id AS target_repo_id
		ORDER BY path, artifact_id
		LIMIT $limit
	`
	deployEvidenceAnchorOldFlux = `
		UNWIND $artifact_ids AS artifact_id
		MATCH (artifact:EvidenceArtifact {id: artifact_id})<-[sourceRel:HAS_DEPLOYMENT_EVIDENCE]-(repo:Repository)
		MATCH (artifact)-[targetRel:EVIDENCES_REPOSITORY_RELATIONSHIP]->(targetRepo:Repository {id: $repo_id})
		WHERE sourceRel.relationship_type = 'DEPLOYS_FROM'
		  AND targetRel.relationship_type = 'DEPLOYS_FROM'
		  AND repo.id IN $source_repo_ids
		RETURN repo.id AS source_id, targetRepo.id AS target_id,
		       artifact.flux_git_repository_namespace AS flux_git_repository_namespace,
		       artifact.flux_git_repository_name AS flux_git_repository_name
	`
	deployEvidenceAnchorCandidateFlux = `
		UNWIND $artifact_ids AS artifact_id
		MATCH (artifact:EvidenceArtifact {id: artifact_id})<-[sourceRel:HAS_DEPLOYMENT_EVIDENCE]-(repo:Repository)
		MATCH (targetRepo:Repository {id: $repo_id})<-[targetRel:EVIDENCES_REPOSITORY_RELATIONSHIP]-(artifact)
		WHERE sourceRel.relationship_type = 'DEPLOYS_FROM'
		  AND targetRel.relationship_type = 'DEPLOYS_FROM'
		  AND repo.id IN $source_repo_ids
		RETURN repo.id AS source_id, targetRepo.id AS target_id,
		       artifact.flux_git_repository_namespace AS flux_git_repository_namespace,
		       artifact.flux_git_repository_name AS flux_git_repository_name
	`
)

func deployEvidenceAnchorRepoID(kind string, i int) string {
	return fmt.Sprintf("%s%s-%04d", deployEvidenceAnchorPrefix, kind, i)
}

func deployEvidenceAnchorFillerCount(t *testing.T) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(deployEvidenceAnchorFillerEnv))
	if raw == "" {
		return deployEvidenceAnchorDefaultFiller
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 6 {
		t.Fatalf("%s=%q must be an integer >= 6", deployEvidenceAnchorFillerEnv, raw)
	}
	return n
}

// deployEvidenceAnchorArtifactRow builds one row shaped exactly like the
// production upsert (storagecypher.BatchCanonicalRepoEvidenceArtifactUpsertCypher
// -- see internal/storage/cypher/canonical_relationships.go), flux-shaped so
// the same 40 hub artifacts serve both the deployment-evidence-incoming and
// the flux-expansion comparisons.
func deployEvidenceAnchorArtifactRow(sourceRepoID, targetRepoID, artifactID, path string) map[string]any {
	return map[string]any{
		"repo_id":                       sourceRepoID,
		"target_repo_id":                targetRepoID,
		"artifact_id":                   artifactID,
		"name":                          artifactID,
		"path":                          path,
		"evidence_kind":                 "FLUX_GIT_REPOSITORY_SOURCE",
		"artifact_family":               "flux",
		"extractor":                     "flux-kustomization",
		"relationship_type":             "DEPLOYS_FROM",
		"resolved_id":                   sourceRepoID,
		"generation_id":                 "gen-6811",
		"confidence":                    0.9,
		"environment":                   "prod",
		"runtime_platform_kind":         "kubernetes",
		"matched_alias":                 "",
		"matched_value":                 "",
		"flux_git_repository_name":      artifactID,
		"flux_git_repository_namespace": "flux-system",
		"evidence_source":               "test/6811-anchor",
		"start_line":                    1,
		"end_line":                      5,
		"commit_sha":                    "deadbeef6811",
		"ref_value":                     "main",
		"ref_pinned":                    false,
	}
}

// deployEvidenceAnchorFillerMeshTarget picks the filler index each filler
// repo's single noise artifact points at, skipping index 0 (the mid-graph
// repo) so only the two explicit mid rows below ever target it -- the mid
// repo's incoming count stays exactly 2 by construction regardless of N.
func deployEvidenceAnchorFillerMeshTarget(i, n int) int {
	target := (i + 1) % n
	if target == 0 {
		target = 1
	}
	return target
}

type deployEvidenceAnchorSeed struct {
	hubID       string
	leafID      string
	midID       string
	srcIDs      []string
	fillerCount int
}

func (r repoLiveReader) writeParams(ctx context.Context, t *testing.T, cypher string, params map[string]any) {
	t.Helper()
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("write params %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume write params %q: %v", cypher, err)
	}
}

func seedDeployEvidenceAnchor(ctx context.Context, t *testing.T, reader repoLiveReader, fillerCount int) deployEvidenceAnchorSeed {
	t.Helper()
	seed := deployEvidenceAnchorSeed{
		hubID:       deployEvidenceAnchorPrefix + "hub",
		leafID:      deployEvidenceAnchorPrefix + "leaf",
		midID:       deployEvidenceAnchorRepoID("filler", 0),
		fillerCount: fillerCount,
	}
	for i := 0; i < deployEvidenceAnchorHubIncoming; i++ {
		seed.srcIDs = append(seed.srcIDs, deployEvidenceAnchorRepoID("src", i))
	}

	// 1) Repository nodes: hub, leaf, 40 source repos, N filler repos.
	repoIDs := append([]string{seed.hubID, seed.leafID}, seed.srcIDs...)
	for i := 0; i < fillerCount; i++ {
		repoIDs = append(repoIDs, deployEvidenceAnchorRepoID("filler", i))
	}
	for _, chunk := range chunkItems(repoIDs, deployEvidenceAnchorWriteBatch) {
		reader.writeParams(ctx, t, `UNWIND $ids AS id CREATE (:Repository {id: id})`, map[string]any{"ids": chunk})
	}

	// 2) Hub's 40 known-by-construction incoming artifacts, plus mid's 2,
	// shaped exactly like the production upsert.
	fillerAIdx, fillerBIdx := fillerCount/3, (2*fillerCount)/3
	if fillerAIdx == 0 {
		fillerAIdx = 1
	}
	if fillerBIdx == 0 || fillerBIdx == fillerAIdx {
		fillerBIdx = fillerAIdx + 1
	}
	var coreRows []map[string]any
	for i, src := range seed.srcIDs {
		coreRows = append(coreRows, deployEvidenceAnchorArtifactRow(
			src, seed.hubID,
			fmt.Sprintf("%sart-hub-%04d", deployEvidenceAnchorPrefix, i),
			fmt.Sprintf("deploy/hub-%04d.yaml", i),
		))
	}
	coreRows = append(coreRows,
		deployEvidenceAnchorArtifactRow(deployEvidenceAnchorRepoID("filler", fillerAIdx), seed.midID,
			fmt.Sprintf("%sart-mid-0000", deployEvidenceAnchorPrefix), "deploy/mid-0000.yaml"),
		deployEvidenceAnchorArtifactRow(deployEvidenceAnchorRepoID("filler", fillerBIdx), seed.midID,
			fmt.Sprintf("%sart-mid-0001", deployEvidenceAnchorPrefix), "deploy/mid-0001.yaml"),
	)
	for _, chunk := range chunkItems(coreRows, deployEvidenceAnchorWriteBatch) {
		reader.writeParams(ctx, t, storagecypher.BatchCanonicalRepoEvidenceArtifactUpsertCypher, map[string]any{"rows": chunk})
	}

	// 3) Filler mesh: each filler repo has one noise artifact pointing at
	// ANOTHER filler repo (never at the hub, leaf, or mid), so graph size
	// grows without changing any of the three known answers.
	var fillerRows []map[string]any
	for i := 0; i < fillerCount; i++ {
		target := deployEvidenceAnchorFillerMeshTarget(i, fillerCount)
		fillerRows = append(fillerRows, map[string]any{
			"repo_id":        deployEvidenceAnchorRepoID("filler", i),
			"target_repo_id": deployEvidenceAnchorRepoID("filler", target),
			"artifact_id":    fmt.Sprintf("%sart-filler-%04d", deployEvidenceAnchorPrefix, i),
			"path":           fmt.Sprintf("filler/%04d.yaml", i),
		})
	}
	const fillerMeshCypher = `
		UNWIND $rows AS row
		MATCH (source_repo:Repository {id: row.repo_id})
		MATCH (target_repo:Repository {id: row.target_repo_id})
		CREATE (artifact:EvidenceArtifact {id: row.artifact_id, path: row.path})
		CREATE (source_repo)-[:HAS_DEPLOYMENT_EVIDENCE]->(artifact)
		CREATE (artifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(target_repo)
	`
	for _, chunk := range chunkItems(fillerRows, deployEvidenceAnchorWriteBatch) {
		reader.writeParams(ctx, t, fillerMeshCypher, map[string]any{"rows": chunk})
	}
	return seed
}

// chunkItems splits items into size-bounded slices so a large seed writes in
// UNWIND-batched statements instead of one unbounded UNWIND.
func chunkItems[T any](items []T, size int) [][]T {
	var out [][]T
	for len(items) > 0 {
		n := size
		if n > len(items) {
			n = len(items)
		}
		out = append(out, items[:n])
		items = items[n:]
	}
	return out
}

type evidenceRowKey struct{ artifactID, sourceID string }

func evidenceRowKeysFromRaw(rows []map[string]any) []evidenceRowKey {
	keys := make([]evidenceRowKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, evidenceRowKey{
			artifactID: querycontract.StringVal(row, "artifact_id"),
			sourceID:   querycontract.StringVal(row, "source_repo_id"),
		})
	}
	return sortedEvidenceRowKeys(keys)
}

func evidenceRowKeysFromArtifacts(artifacts []map[string]any) []evidenceRowKey {
	keys := make([]evidenceRowKey, 0, len(artifacts))
	for _, a := range artifacts {
		if querycontract.StringVal(a, "direction") != "incoming" {
			continue
		}
		keys = append(keys, evidenceRowKey{
			artifactID: querycontract.StringVal(a, "id"),
			sourceID:   querycontract.StringVal(a, "source_repo_id"),
		})
	}
	return sortedEvidenceRowKeys(keys)
}

func sortedEvidenceRowKeys(keys []evidenceRowKey) []evidenceRowKey {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].artifactID != keys[j].artifactID {
			return keys[i].artifactID < keys[j].artifactID
		}
		return keys[i].sourceID < keys[j].sourceID
	})
	return keys
}

func keysEqual(a, b []evidenceRowKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func productionArtifacts(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	if result == nil {
		return nil
	}
	raw, _ := result["artifacts"].([]map[string]any)
	return raw
}

func TestLiveNornicDBDeploymentEvidenceAnchor(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	fillerCount := deployEvidenceAnchorFillerCount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	reader := repoLiveReader{driver: driver}
	reader.write(ctx, t, deployEvidenceAnchorCleanup)
	seed := seedDeployEvidenceAnchor(ctx, t, reader, fillerCount)
	defer reader.write(context.Background(), t, deployEvidenceAnchorCleanup)

	t.Run("incoming read row-set truth", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			repoID  string
			wantLen int
		}{
			{"hub", seed.hubID, deployEvidenceAnchorHubIncoming},
			{"leaf", seed.leafID, 0},
			{"mid", seed.midID, deployEvidenceAnchorMidIncoming},
		} {
			t.Run(tc.name, func(t *testing.T) {
				params := map[string]any{"repo_id": tc.repoID}

				prodResult, err := QueryRepoDeploymentEvidence(ctx, reader, nil, params)
				if err != nil {
					t.Fatalf("QueryRepoDeploymentEvidence: %v", err)
				}
				prodKeys := evidenceRowKeysFromArtifacts(productionArtifacts(t, prodResult))

				oldParams := map[string]any{"repo_id": tc.repoID, "limit": 1000}
				oldRows, err := reader.Run(ctx, deployEvidenceAnchorOldIncoming, oldParams)
				if err != nil {
					t.Fatalf("old incoming statement: %v", err)
				}
				oldKeys := evidenceRowKeysFromRaw(oldRows)

				candidateRows, err := reader.Run(ctx, deployEvidenceAnchorCandidateIncoming, oldParams)
				if err != nil {
					t.Fatalf("candidate incoming statement: %v", err)
				}
				candidateKeys := evidenceRowKeysFromRaw(candidateRows)

				t.Logf("%s: production=%d rows, old-statement=%d rows, candidate=%d rows, want=%d",
					tc.name, len(prodKeys), len(oldKeys), len(candidateKeys), tc.wantLen)

				// The candidate is the truth assertion: it must equal the
				// known-by-construction answer.
				if len(candidateKeys) != tc.wantLen {
					t.Fatalf("%s: candidate returned %d rows, want %d (known by construction)", tc.name, len(candidateKeys), tc.wantLen)
				}
				// Production currently runs the pre-change statement, so it
				// must track old-statement exactly (same code path).
				if !keysEqual(prodKeys, oldKeys) {
					t.Fatalf("%s: production artifacts (%d) != old-statement rows (%d) -- production should be running the pre-change statement verbatim",
						tc.name, len(prodKeys), len(oldKeys))
				}
				// This is the #6811 finding, not a test failure: log whether
				// the pre-change (right-anchored) shape already disagrees
				// with the bound-anchored truth.
				if !keysEqual(oldKeys, candidateKeys) {
					t.Logf("#6811 FINDING for %s: pre-change statement returned %d rows %v, candidate/truth returned %d rows %v -- accuracy bug confirmed on this build",
						tc.name, len(oldKeys), oldKeys, len(candidateKeys), candidateKeys)
				} else {
					t.Logf("%s: pre-change statement matches candidate/truth (%d rows) -- no accuracy divergence observed on this build", tc.name, len(oldKeys))
				}
			})
		}
	})

	t.Run("flux expansion row-set truth", func(t *testing.T) {
		access := querycontract.RepositoryAccessFilter{AllScopes: true}

		prodResult, err := impacttrace.FetchFluxDeploymentSourceTargetBindings(ctx, reader, seed.hubID, seed.srcIDs, 1000, access)
		if err != nil {
			t.Fatalf("FetchFluxDeploymentSourceTargetBindings: %v", err)
		}
		prodKeys := fluxRowKeys(prodResult.Rows())

		artifactIDs := make([]string, 0, len(seed.srcIDs))
		for i := range seed.srcIDs {
			artifactIDs = append(artifactIDs, fmt.Sprintf("%sart-hub-%04d", deployEvidenceAnchorPrefix, i))
		}
		sort.Strings(artifactIDs)
		fluxParams := map[string]any{"repo_id": seed.hubID, "artifact_ids": artifactIDs, "source_repo_ids": seed.srcIDs}

		oldRows, err := reader.Run(ctx, deployEvidenceAnchorOldFlux, fluxParams)
		if err != nil {
			t.Fatalf("old flux statement: %v", err)
		}
		oldKeys := fluxRowKeys(oldRows)

		candidateRows, err := reader.Run(ctx, deployEvidenceAnchorCandidateFlux, fluxParams)
		if err != nil {
			t.Fatalf("candidate flux statement: %v", err)
		}
		candidateKeys := fluxRowKeys(candidateRows)

		t.Logf("flux hub: production=%d rows, old-statement=%d rows, candidate=%d rows, want=%d",
			len(prodKeys), len(oldKeys), len(candidateKeys), deployEvidenceAnchorHubIncoming)

		if len(candidateKeys) != deployEvidenceAnchorHubIncoming {
			t.Fatalf("flux hub: candidate returned %d rows, want %d (known by construction)", len(candidateKeys), deployEvidenceAnchorHubIncoming)
		}
		if !keysEqual(prodKeys, oldKeys) {
			t.Fatalf("flux hub: production bindings (%d) != old-statement rows (%d) -- production should be running the pre-change statement verbatim",
				len(prodKeys), len(oldKeys))
		}
		if !keysEqual(oldKeys, candidateKeys) {
			t.Logf("#6811 FINDING for flux hub: pre-change statement returned %d rows %v, candidate/truth returned %d rows %v -- accuracy bug confirmed on this build",
				len(oldKeys), oldKeys, len(candidateKeys), candidateKeys)
		} else {
			t.Logf("flux hub: pre-change statement matches candidate/truth (%d rows) -- no accuracy divergence observed on this build", len(oldKeys))
		}
	})
}

// fluxRowKeys reduces flux binding rows to (source_id,target_id) pairs, which
// is what both the production function and the raw statements return.
func fluxRowKeys(rows []map[string]any) []evidenceRowKey {
	keys := make([]evidenceRowKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, evidenceRowKey{
			artifactID: querycontract.StringVal(row, "target_id"),
			sourceID:   querycontract.StringVal(row, "source_id"),
		})
	}
	return sortedEvidenceRowKeys(keys)
}
