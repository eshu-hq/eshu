// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

const (
	provenanceEdgeIdentityLiveSamples    = 5
	provenanceEdgeIdentityLiveConcurrent = 8
)

type provenanceEdgeIdentityLiveCase struct {
	name    string
	seed    []string
	cleanup []string
	legacy  string
	read    string
	sourceA string
	sourceB string
	sourceC string
	row     func(map[string]any) map[string]any
	write   func(*ProvenanceEdgeWriter, context.Context, []map[string]any, string, string, string) error
	retract func(*ProvenanceEdgeWriter, context.Context, string, string, string) error
}

// TestProvenanceEdgeWriterLiveLegacyRowSetMigration starts from the single
// endpoint-only relationship shape written before #5827, then replays both
// current assertions through the production retract-before-write flow. Both
// replay orders must replace the collapsed one-row set with the exact two-row
// source-of-truth set.
func TestProvenanceEdgeWriterLiveLegacyRowSetMigration(t *testing.T) {
	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	writer := NewProvenanceEdgeWriter(&RetryingExecutor{
		Inner:      &boltTestExecutor{runner: runner},
		MaxRetries: 3,
		BaseDelay:  5 * time.Millisecond,
	}, 0)
	for caseIndex, liveCase := range provenanceEdgeIdentityLiveCases() {
		for orderIndex, replayOrder := range [][]int{{0, 1}, {1, 0}} {
			name := fmt.Sprintf("%s/order_%d_then_%d", liveCase.name, replayOrder[0], replayOrder[1])
			t.Run(name, func(t *testing.T) {
				nonce := time.Now().UnixNano() + int64(caseIndex*10+orderIndex)
				params := provenanceEdgeIdentityLiveParams(nonce)
				seedProvenanceEdgeIdentityLiveEndpoints(ctx, t, runner, liveCase, params, nonce)
				scopeA := fmt.Sprintf("migration-5827-%s-%d-a", liveCase.name, nonce)
				scopeB := fmt.Sprintf("migration-5827-%s-%d-b", liveCase.name, nonce)
				params["legacy_scope_id"] = scopeB
				params["legacy_evidence_source"] = liveCase.sourceB
				if err := boltWriteStatement(ctx, runner, liveCase.legacy, params); err != nil {
					t.Fatalf("seed legacy collapsed assertion: %v", err)
				}
				assertProvenanceEdgeIdentityPairs(ctx, t, runner, liveCase.read, params, map[string]string{
					scopeB: liveCase.sourceB,
				})

				rows := []map[string]any{liveCase.row(params)}
				assertions := []struct {
					scopeID string
					source  string
				}{
					{scopeID: scopeA, source: liveCase.sourceA},
					{scopeID: scopeB, source: liveCase.sourceB},
				}
				for _, assertionIndex := range replayOrder {
					assertion := assertions[assertionIndex]
					if err := liveCase.retract(writer, ctx, assertion.scopeID, "migration-replay", assertion.source); err != nil {
						t.Fatalf("retract assertion %d during migration: %v", assertionIndex, err)
					}
					if err := liveCase.write(writer, ctx, rows, assertion.scopeID, "migration-replay", assertion.source); err != nil {
						t.Fatalf("rebuild assertion %d during migration: %v", assertionIndex, err)
					}
				}
				want := map[string]string{scopeA: liveCase.sourceA, scopeB: liveCase.sourceB}
				assertProvenanceEdgeIdentityPairs(ctx, t, runner, liveCase.read, params, want)
				t.Logf("row-set migration edge=%s before=1 after=%d", liveCase.name, len(want))
			})
		}
	}
}

// TestProvenanceEdgeWriterLiveSamePairAssertionIsolation proves relationship
// identity, duplicate-delivery, concurrent-delivery, and retract isolation for
// every provenance edge writer against the configured live graph backend.
func TestProvenanceEdgeWriterLiveSamePairAssertionIsolation(t *testing.T) {
	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	writer := NewProvenanceEdgeWriter(&RetryingExecutor{
		Inner:      &boltTestExecutor{runner: runner},
		MaxRetries: 3,
		BaseDelay:  5 * time.Millisecond,
	}, 0)
	for _, liveCase := range provenanceEdgeIdentityLiveCases() {
		t.Run(liveCase.name, func(t *testing.T) {
			for sample := 0; sample < provenanceEdgeIdentityLiveSamples; sample++ {
				runProvenanceEdgeIdentityLiveSample(ctx, t, runner, writer, liveCase, sample)
			}
		})
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("live relationship identity proof exceeded its time bound: %v", err)
	}
}

func runProvenanceEdgeIdentityLiveSample(
	ctx context.Context,
	t *testing.T,
	runner *boltRetractTestRunner,
	writer *ProvenanceEdgeWriter,
	liveCase provenanceEdgeIdentityLiveCase,
	sample int,
) {
	t.Helper()
	started := time.Now()
	nonce := time.Now().UnixNano() + int64(sample)
	params := provenanceEdgeIdentityLiveParams(nonce)
	seedProvenanceEdgeIdentityLiveEndpoints(ctx, t, runner, liveCase, params, int64(sample))

	rows := []map[string]any{liveCase.row(params)}
	scopeA := fmt.Sprintf("prove-5827-%s-%d-a", liveCase.name, nonce)
	scopeB := fmt.Sprintf("prove-5827-%s-%d-b", liveCase.name, nonce)
	scopeC := fmt.Sprintf("prove-5827-%s-%d-c", liveCase.name, nonce)
	if err := liveCase.write(writer, ctx, rows, scopeA, "generation-a", liveCase.sourceA); err != nil {
		t.Fatalf("sample %d write assertion A: %v", sample, err)
	}
	if err := liveCase.write(writer, ctx, rows, scopeB, "generation-b", liveCase.sourceB); err != nil {
		t.Fatalf("sample %d write assertion B: %v", sample, err)
	}
	if err := liveCase.write(writer, ctx, rows, scopeA, "generation-a-retry", liveCase.sourceA); err != nil {
		t.Fatalf("sample %d duplicate assertion A: %v", sample, err)
	}
	assertProvenanceEdgeIdentityPairs(ctx, t, runner, liveCase.read, params, map[string]string{
		scopeA: liveCase.sourceA,
		scopeB: liveCase.sourceB,
	})

	writeProvenanceEdgeIdentityConcurrently(ctx, t, writer, liveCase, rows, scopeC)
	assertProvenanceEdgeIdentityPairs(ctx, t, runner, liveCase.read, params, map[string]string{
		scopeA: liveCase.sourceA,
		scopeB: liveCase.sourceB,
		scopeC: liveCase.sourceC,
	})

	if err := liveCase.retract(writer, ctx, scopeB, "generation-b", liveCase.sourceB); err != nil {
		t.Fatalf("sample %d retract assertion B: %v", sample, err)
	}
	assertProvenanceEdgeIdentityPairs(ctx, t, runner, liveCase.read, params, map[string]string{
		scopeA: liveCase.sourceA,
		scopeC: liveCase.sourceC,
	})
	t.Logf("sample=%d edge=%s duration=%s", sample, liveCase.name, time.Since(started))
}

func seedProvenanceEdgeIdentityLiveEndpoints(
	ctx context.Context,
	t *testing.T,
	runner *boltRetractTestRunner,
	liveCase provenanceEdgeIdentityLiveCase,
	params map[string]any,
	sample int64,
) {
	t.Helper()
	for _, cypher := range liveCase.seed {
		if err := boltWriteStatement(ctx, runner, cypher, params); err != nil {
			t.Fatalf("sample %d seed %s endpoints: %v", sample, liveCase.name, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, cypher := range liveCase.cleanup {
			if err := boltWriteStatement(cleanupCtx, runner, cypher, params); err != nil {
				t.Errorf("sample %d clean up %s endpoints: %v", sample, liveCase.name, err)
			}
		}
	})
}

func writeProvenanceEdgeIdentityConcurrently(
	ctx context.Context,
	t *testing.T,
	writer *ProvenanceEdgeWriter,
	liveCase provenanceEdgeIdentityLiveCase,
	rows []map[string]any,
	scopeID string,
) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, provenanceEdgeIdentityLiveConcurrent)
	var ready sync.WaitGroup
	var writers sync.WaitGroup
	ready.Add(provenanceEdgeIdentityLiveConcurrent)
	for i := 0; i < provenanceEdgeIdentityLiveConcurrent; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			ready.Done()
			<-start
			errs <- liveCase.write(writer, ctx, rows, scopeID, "generation-concurrent", liveCase.sourceC)
		}()
	}
	ready.Wait()
	close(start)
	writers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent same-identity delivery: %v", err)
		}
	}
}

func assertProvenanceEdgeIdentityPairs(
	ctx context.Context,
	t *testing.T,
	runner *boltRetractTestRunner,
	cypher string,
	params map[string]any,
	want map[string]string,
) {
	t.Helper()
	rows, err := runner.runCypher(ctx, cypher, params)
	if err != nil {
		t.Fatalf("read provenance assertions: %v", err)
	}
	got := provenanceAssertionPairs(rows)
	if len(rows) != len(want) || !equalStringMaps(got, want) {
		t.Fatalf("provenance assertions = %v across %d rows, want %v across %d rows", got, len(rows), want, len(want))
	}
}

func provenanceEdgeIdentityLiveCases() []provenanceEdgeIdentityLiveCase {
	return []provenanceEdgeIdentityLiveCase{
		{
			name: "publishes",
			seed: []string{
				`CREATE (:Repository {id: $repository_id})`,
				`CREATE (:Package:PackageRegistryPackage {uid: $package_id})`,
			},
			cleanup: []string{
				`MATCH (node:Repository {id: $repository_id}) DETACH DELETE node`,
				`MATCH (node:Package {uid: $package_id}) DETACH DELETE node`,
			},
			legacy: `MATCH (repo:Repository {id: $repository_id})
MATCH (target:Package {uid: $package_id})
MERGE (repo)-[rel:PUBLISHES]->(target)
SET rel.scope_id = $legacy_scope_id, rel.evidence_source = $legacy_evidence_source`,
			read: `MATCH (:Repository {id: $repository_id})-[rel:PUBLISHES]->(:Package {uid: $package_id})
RETURN rel.scope_id AS scope_id, rel.evidence_source AS evidence_source`,
			sourceA: "reducer/package-ownership",
			sourceB: "reducer/package-publication",
			sourceC: "reducer/package-attestation",
			row: func(params map[string]any) map[string]any {
				return map[string]any{
					"repository_id": params["repository_id"],
					"package_id":    params["package_id"],
				}
			},
			write:   (*ProvenanceEdgeWriter).WritePublishesEdges,
			retract: (*ProvenanceEdgeWriter).RetractPublishesEdges,
		},
		{
			name: "publishes_version",
			seed: []string{
				`CREATE (:Repository {id: $repository_id})`,
				`CREATE (:PackageVersion:PackageRegistryPackageVersion {uid: $version_id})`,
			},
			cleanup: []string{
				`MATCH (node:Repository {id: $repository_id}) DETACH DELETE node`,
				`MATCH (node:PackageVersion {uid: $version_id}) DETACH DELETE node`,
			},
			legacy: `MATCH (repo:Repository {id: $repository_id})
MATCH (target:PackageVersion {uid: $version_id})
MERGE (repo)-[rel:PUBLISHES]->(target)
SET rel.scope_id = $legacy_scope_id, rel.evidence_source = $legacy_evidence_source`,
			read: `MATCH (:Repository {id: $repository_id})-[rel:PUBLISHES]->(:PackageVersion {uid: $version_id})
RETURN rel.scope_id AS scope_id, rel.evidence_source AS evidence_source`,
			sourceA: "reducer/package-ownership",
			sourceB: "reducer/package-publication",
			sourceC: "reducer/package-attestation",
			row: func(params map[string]any) map[string]any {
				return map[string]any{
					"repository_id": params["repository_id"],
					"version_id":    params["version_id"],
				}
			},
			write:   (*ProvenanceEdgeWriter).WritePublishesEdges,
			retract: (*ProvenanceEdgeWriter).RetractPublishesEdges,
		},
		{
			name: "built_from",
			seed: []string{
				`CREATE (:ContainerImage:OciImageManifest {uid: $image_uid, digest: $digest})`,
				`CREATE (:Repository {id: $repository_id})`,
			},
			cleanup: []string{
				`MATCH (node:ContainerImage {digest: $digest}) DETACH DELETE node`,
				`MATCH (node:Repository {id: $repository_id}) DETACH DELETE node`,
			},
			legacy: `MATCH (img:ContainerImage {digest: $digest})
MATCH (repo:Repository {id: $repository_id})
MERGE (img)-[rel:BUILT_FROM]->(repo)
SET rel.scope_id = $legacy_scope_id, rel.evidence_source = $legacy_evidence_source`,
			read: `MATCH (:ContainerImage {digest: $digest})-[rel:BUILT_FROM]->(:Repository {id: $repository_id})
RETURN rel.scope_id AS scope_id, rel.evidence_source AS evidence_source`,
			sourceA: "reducer/container-image-identity",
			sourceB: "reducer/ci-cd-run-correlation",
			sourceC: "reducer/slsa-provenance",
			row: func(params map[string]any) map[string]any {
				return map[string]any{
					"digest":        params["digest"],
					"repository_id": params["repository_id"],
				}
			},
			write:   (*ProvenanceEdgeWriter).WriteBuiltFromEdges,
			retract: (*ProvenanceEdgeWriter).RetractBuiltFromEdges,
		},
		{
			name: "derived_from",
			seed: []string{
				`CREATE (:ContainerImage:OciImageManifest {uid: $image_uid, digest: $digest})`,
				`CREATE (:ContainerImage:OciImageManifest {uid: $base_uid, digest: $base_digest})`,
			},
			cleanup: []string{
				`MATCH (node:ContainerImage {digest: $digest}) DETACH DELETE node`,
				`MATCH (node:ContainerImage {digest: $base_digest}) DETACH DELETE node`,
			},
			legacy: `MATCH (img:ContainerImage {digest: $digest})
MATCH (base:ContainerImage {digest: $base_digest})
MERGE (img)-[rel:DERIVED_FROM]->(base)
SET rel.scope_id = $legacy_scope_id, rel.evidence_source = $legacy_evidence_source`,
			read: `MATCH (:ContainerImage {digest: $digest})-[rel:DERIVED_FROM]->(:ContainerImage {digest: $base_digest})
RETURN rel.scope_id AS scope_id, rel.evidence_source AS evidence_source`,
			sourceA: "reducer/container-image-base-image",
			sourceB: "reducer/slsa-provenance",
			sourceC: "reducer/ci-cd-run-correlation",
			row: func(params map[string]any) map[string]any {
				return map[string]any{
					"digest":            params["digest"],
					"base_digest":       params["base_digest"],
					"attribution_basis": "dockerfile-from",
				}
			},
			write:   (*ProvenanceEdgeWriter).WriteDerivedFromEdges,
			retract: (*ProvenanceEdgeWriter).RetractDerivedFromEdges,
		},
	}
}

func provenanceEdgeIdentityLiveParams(nonce int64) map[string]any {
	return map[string]any{
		"repository_id": fmt.Sprintf("repository:prove-5827-%d", nonce),
		"package_id":    fmt.Sprintf("package:prove-5827-%d", nonce),
		"version_id":    fmt.Sprintf("package-version:prove-5827-%d", nonce),
		"digest":        fmt.Sprintf("sha256:%064x", nonce),
		"base_digest":   fmt.Sprintf("sha256:%064x", nonce+1),
		"image_uid":     fmt.Sprintf("oci-manifest:prove-5827-%d", nonce),
		"base_uid":      fmt.Sprintf("oci-manifest:prove-5827-base-%d", nonce),
	}
}

func provenanceAssertionPairs(rows []map[string]any) map[string]string {
	pairs := make(map[string]string, len(rows))
	for _, row := range rows {
		scopeID, _ := row["scope_id"].(string)
		evidenceSource, _ := row["evidence_source"].(string)
		pairs[scopeID] = evidenceSource
	}
	return pairs
}

func equalStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
