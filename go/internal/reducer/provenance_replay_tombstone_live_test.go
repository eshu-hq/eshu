// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	provenanceReplayScopeID         = "replay-provenance:scope-in"
	provenanceReplayPackageRepoID   = "repository:replay-provenance-package-in"
	provenanceReplayBuildRepoID     = "repository:replay-provenance-build-in"
	provenanceReplayPackageID       = "pkg:npm/replay-provenance"
	provenanceReplayVersionID       = "pkg:npm/replay-provenance@1.0.0"
	provenanceReplayContainerDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	provenanceReplayOutScopeID      = "replay-provenance:scope-out"
	provenanceReplayOutPackageRepo  = "repository:replay-provenance-package-out"
	provenanceReplayOutVersionID    = "pkg:npm/replay-provenance-out@1.0.0"
	provenanceReplayOutBuildRepo    = "repository:replay-provenance-build-out"
	provenanceReplayOutDigest       = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

var provenanceReplayCassettePath = filepath.Join(
	"..", "..", "..", "testdata", "cassettes", "replaydelta", "provenance-edges-tombstone.json",
)

type provenanceReplayGeneration struct {
	scopeID      string
	generationID string
	facts        []facts.Envelope
}

// TestProvenanceReplayTombstoneCassetteDecisions is the credential-free half
// of the replay proof. It prevents the live graph assertions from passing on
// an empty or incorrectly-shaped cassette.
func TestProvenanceReplayTombstoneCassetteDecisions(t *testing.T) {
	t.Parallel()

	gen1, gen2 := loadProvenanceReplayGenerations(t)
	if gen1.scopeID != provenanceReplayScopeID || gen2.scopeID != provenanceReplayScopeID {
		t.Fatalf("scope ids = %q, %q, want %q", gen1.scopeID, gen2.scopeID, provenanceReplayScopeID)
	}

	packageGen1 := reducer.BuildPackageSourceCorrelationDecisions(gen1.facts)
	if got, want := len(packageGen1), 1; got != want {
		t.Fatalf("generation 1 package decisions = %d, want %d", got, want)
	}
	if got := reducer.PackageOwnershipPublishesRowsForReplayTest(packageGen1); len(got) != 1 ||
		got[0]["repository_id"] != provenanceReplayPackageRepoID ||
		got[0]["package_id"] != provenanceReplayPackageID {
		t.Fatalf("generation 1 ownership PUBLISHES rows = %#v, want one package row", got)
	}
	publicationGen1 := reducer.BuildPackagePublicationDecisions(gen1.facts)
	if got := reducer.PackagePublicationPublishesRowsForReplayTest(publicationGen1); len(got) != 1 ||
		got[0]["repository_id"] != provenanceReplayPackageRepoID ||
		got[0]["version_id"] != provenanceReplayVersionID {
		t.Fatalf("generation 1 publication PUBLISHES rows = %#v, want one package-version row", got)
	}
	containerGen1 := reducer.BuildContainerImageIdentityDecisions(gen1.facts)
	if got := reducer.ContainerImageBuiltFromRowsForReplayTest(containerGen1); len(got) != 1 ||
		got[0]["digest"] != provenanceReplayContainerDigest ||
		got[0]["repository_id"] != provenanceReplayBuildRepoID {
		t.Fatalf("generation 1 BUILT_FROM rows = %#v, want one build-source row", got)
	}

	if got := reducer.BuildPackageSourceCorrelationDecisions(gen2.facts); len(got) != 0 {
		t.Fatalf("generation 2 package decisions = %#v, want none", got)
	}
	if got := reducer.BuildPackagePublicationDecisions(gen2.facts); len(got) != 0 {
		t.Fatalf("generation 2 publication decisions = %#v, want none", got)
	}
	if got := reducer.ContainerImageBuiltFromRowsForReplayTest(
		reducer.BuildContainerImageIdentityDecisions(gen2.facts),
	); len(got) != 0 {
		t.Fatalf("generation 2 BUILT_FROM rows = %#v, want none", got)
	}
	assertProvenanceReplayEndpoints(t, gen2.facts)
}

// TestReducerProvenanceReplayTombstoneGraphTruth proves the full cassette ->
// decision builders -> package-private retract-first projectors -> real writer
// -> NornicDB path. Ordinary go test skips without ESHU_REPLAY_TIER_LIVE.
func TestReducerProvenanceReplayTombstoneGraphTruth(t *testing.T) {
	if !provenanceReplayLiveEnabled() {
		t.Skip("set ESHU_REPLAY_TIER_LIVE=1 and configure the replay-tier NornicDB backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open replay-tier Bolt driver: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = driver.Close(closeCtx)
	})
	executor := newProvenanceReplayExecutor(driver, cfg.DatabaseName)
	cleanupProvenanceReplayGraph(ctx, t, executor)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		cleanupProvenanceReplayGraph(cleanupCtx, t, executor)
	})
	seedProvenanceReplayEndpoints(ctx, t, executor)

	gen1, gen2 := loadProvenanceReplayGenerations(t)
	writer := cypher.NewProvenanceEdgeWriter(executor, 10)
	writeProvenanceReplaySurvivors(ctx, t, writer)
	projectProvenanceReplayGeneration(ctx, t, writer, gen1)
	assertProvenanceReplayGenerationOne(ctx, t, executor)

	projectProvenanceReplayGeneration(ctx, t, writer, gen2)
	assertProvenanceReplayGenerationTwo(ctx, t, executor)
	projectProvenanceReplayGeneration(ctx, t, writer, gen2)
	assertProvenanceReplayGenerationTwo(ctx, t, executor)
}

func provenanceReplayLiveEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_REPLAY_TIER_LIVE"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func cleanupProvenanceReplayGraph(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	for _, endpoint := range provenanceReplayGraphEndpoints() {
		query := fmt.Sprintf("MATCH (node:%s {%s: $value}) DETACH DELETE node", endpoint.label, endpoint.key)
		if err := executor.Execute(ctx, cypher.Statement{
			Cypher: query, Parameters: map[string]any{"value": endpoint.value},
		}); err != nil {
			t.Fatalf("clean provenance replay %s endpoint: %v", endpoint.label, err)
		}
	}
}

func seedProvenanceReplayEndpoints(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	for _, endpoint := range provenanceReplayGraphEndpoints() {
		query := fmt.Sprintf("MERGE (node:%s {%s: $value})", endpoint.label, endpoint.key)
		if err := executor.Execute(ctx, cypher.Statement{
			Cypher: query, Parameters: map[string]any{"value": endpoint.value},
		}); err != nil {
			t.Fatalf("seed provenance replay endpoint: %v", err)
		}
	}
}

type provenanceReplayEndpoint struct {
	label string
	key   string
	value string
}

func provenanceReplayGraphEndpoints() []provenanceReplayEndpoint {
	return []provenanceReplayEndpoint{
		{label: "Repository", key: "id", value: provenanceReplayPackageRepoID},
		{label: "Package", key: "uid", value: provenanceReplayPackageID},
		{label: "PackageVersion", key: "uid", value: provenanceReplayVersionID},
		{label: "Repository", key: "id", value: provenanceReplayBuildRepoID},
		{label: "ContainerImage", key: "digest", value: provenanceReplayContainerDigest},
		{label: "Repository", key: "id", value: provenanceReplayOutPackageRepo},
		{label: "PackageVersion", key: "uid", value: provenanceReplayOutVersionID},
		{label: "Repository", key: "id", value: provenanceReplayOutBuildRepo},
		{label: "ContainerImage", key: "digest", value: provenanceReplayOutDigest},
	}
}

func writeProvenanceReplaySurvivors(
	ctx context.Context,
	t *testing.T,
	writer *cypher.ProvenanceEdgeWriter,
) {
	t.Helper()
	if err := writer.WritePublishesEdges(ctx, []map[string]any{{
		"repository_id": provenanceReplayOutPackageRepo,
		"version_id":    provenanceReplayOutVersionID,
	}}, provenanceReplayOutScopeID, "replay-provenance-out-gen1", "reducer/package-ownership"); err != nil {
		t.Fatalf("write out-of-scope PUBLISHES survivor: %v", err)
	}
	if err := writer.WriteBuiltFromEdges(ctx, []map[string]any{{
		"digest":        provenanceReplayOutDigest,
		"repository_id": provenanceReplayOutBuildRepo,
	}}, provenanceReplayOutScopeID, "replay-provenance-out-gen1", "reducer/container-image-identity"); err != nil {
		t.Fatalf("write out-of-scope BUILT_FROM survivor: %v", err)
	}
}

func projectProvenanceReplayGeneration(
	ctx context.Context,
	t *testing.T,
	writer *cypher.ProvenanceEdgeWriter,
	generation provenanceReplayGeneration,
) {
	t.Helper()
	packageDecisions := reducer.BuildPackageSourceCorrelationDecisions(generation.facts)
	publicationDecisions := reducer.BuildPackagePublicationDecisions(generation.facts)
	if err := reducer.ProjectPackageProvenanceEdgesForReplayTest(
		ctx, writer, generation.scopeID, generation.generationID, packageDecisions, publicationDecisions,
	); err != nil {
		t.Fatalf("project %s package provenance: %v", generation.generationID, err)
	}
	containerDecisions := reducer.BuildContainerImageIdentityDecisions(generation.facts)
	if err := reducer.ProjectContainerImageBuiltFromEdgesForReplayTest(
		ctx, writer, generation.scopeID, generation.generationID, containerDecisions,
	); err != nil {
		t.Fatalf("project %s container provenance: %v", generation.generationID, err)
	}
}

func assertProvenanceReplayGenerationOne(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	assertProvenanceReplayRelationship(t, readProvenanceReplayPublishes(
		ctx, t, executor, provenanceReplayPackageRepoID, "Package", "uid", provenanceReplayPackageID,
	), map[string]any{
		"scope_id": provenanceReplayScopeID, "generation_id": "replay-provenance-gen1",
		"evidence_source": "reducer/package-ownership", "evidence_kinds": "PACKAGE_OWNERSHIP_CORRELATION",
		"source_tool": nil,
	})
	assertProvenanceReplayRelationship(t, readProvenanceReplayPublishes(
		ctx, t, executor, provenanceReplayPackageRepoID, "PackageVersion", "uid", provenanceReplayVersionID,
	), map[string]any{
		"scope_id": provenanceReplayScopeID, "generation_id": "replay-provenance-gen1",
		"evidence_source": "reducer/package-publication", "evidence_kinds": "PACKAGE_PUBLICATION_CORRELATION",
		"source_tool": nil,
	})
	assertProvenanceReplayRelationship(t, readProvenanceReplayBuiltFrom(
		ctx, t, executor, provenanceReplayContainerDigest, provenanceReplayBuildRepoID,
	), map[string]any{
		"scope_id": provenanceReplayScopeID, "generation_id": "replay-provenance-gen1",
		"evidence_source": "reducer/container-image-identity", "evidence_kinds": "CONTAINER_IMAGE_IDENTITY_EXACT_DIGEST",
		"source_tool": "oci",
	})
	assertProvenanceReplaySurvivors(ctx, t, executor)
}

func assertProvenanceReplayGenerationTwo(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	if rows := readProvenanceReplayPublishes(ctx, t, executor, provenanceReplayPackageRepoID, "Package", "uid", provenanceReplayPackageID); len(rows) != 0 {
		t.Fatalf("generation 2 retained in-scope ownership PUBLISHES rows: %#v", rows)
	}
	if rows := readProvenanceReplayPublishes(ctx, t, executor, provenanceReplayPackageRepoID, "PackageVersion", "uid", provenanceReplayVersionID); len(rows) != 0 {
		t.Fatalf("generation 2 retained in-scope publication PUBLISHES rows: %#v", rows)
	}
	if rows := readProvenanceReplayBuiltFrom(ctx, t, executor, provenanceReplayContainerDigest, provenanceReplayBuildRepoID); len(rows) != 0 {
		t.Fatalf("generation 2 retained in-scope BUILT_FROM rows: %#v", rows)
	}
	assertProvenanceReplayNode(ctx, t, executor, "Repository", "id", provenanceReplayPackageRepoID)
	assertProvenanceReplayNode(ctx, t, executor, "Package", "uid", provenanceReplayPackageID)
	assertProvenanceReplayNode(ctx, t, executor, "PackageVersion", "uid", provenanceReplayVersionID)
	assertProvenanceReplayNode(ctx, t, executor, "Repository", "id", provenanceReplayBuildRepoID)
	assertProvenanceReplayNode(ctx, t, executor, "ContainerImage", "digest", provenanceReplayContainerDigest)
	assertProvenanceReplaySurvivors(ctx, t, executor)
}

func assertProvenanceReplaySurvivors(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	if rows := readProvenanceReplayPublishes(ctx, t, executor, provenanceReplayOutPackageRepo, "PackageVersion", "uid", provenanceReplayOutVersionID); len(rows) != 1 {
		t.Fatalf("out-of-scope distinct-endpoint PUBLISHES survivor rows = %#v, want one", rows)
	}
	if rows := readProvenanceReplayBuiltFrom(ctx, t, executor, provenanceReplayOutDigest, provenanceReplayOutBuildRepo); len(rows) != 1 {
		t.Fatalf("out-of-scope distinct-endpoint BUILT_FROM survivor rows = %#v, want one", rows)
	}
}

func loadProvenanceReplayGenerations(t *testing.T) (provenanceReplayGeneration, provenanceReplayGeneration) {
	t.Helper()
	source, err := cassette.NewSource(provenanceReplayCassettePath)
	if err != nil {
		t.Fatalf("load provenance replay cassette %s: %v", provenanceReplayCassettePath, err)
	}
	return readProvenanceReplayGeneration(t, source, "generation 1"),
		readProvenanceReplayGeneration(t, source, "generation 2")
}

func readProvenanceReplayGeneration(
	t *testing.T,
	source *cassette.Source,
	label string,
) provenanceReplayGeneration {
	t.Helper()
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("read %s: %v", label, err)
	}
	if !ok {
		t.Fatalf("read %s: cassette yielded no generation", label)
	}
	return provenanceReplayGeneration{
		scopeID:      collected.Scope.ScopeID,
		generationID: collected.Generation.GenerationID,
		facts:        drainProvenanceReplayFacts(t, collected.Facts, collected.FactStreamErr),
	}
}

func drainProvenanceReplayFacts(
	t *testing.T,
	factStream <-chan facts.Envelope,
	streamErr func() error,
) []facts.Envelope {
	t.Helper()
	var envelopes []facts.Envelope
	for envelope := range factStream {
		envelopes = append(envelopes, envelope)
	}
	if streamErr != nil {
		if err := streamErr(); err != nil {
			t.Fatalf("drain cassette facts: %v", err)
		}
	}
	return envelopes
}

func assertProvenanceReplayEndpoints(t *testing.T, envelopes []facts.Envelope) {
	t.Helper()
	want := map[string]bool{
		"repository:" + provenanceReplayPackageRepoID: false,
		"repository:" + provenanceReplayBuildRepoID:   false,
		"package:" + provenanceReplayPackageID:        false,
		"version:" + provenanceReplayVersionID:        false,
		"digest:" + provenanceReplayContainerDigest:   false,
	}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case "repository":
			want["repository:"+replayPayloadString(envelope.Payload, "repo_id")] = true
		case facts.PackageRegistryPackageFactKind:
			want["package:"+replayPayloadString(envelope.Payload, "package_id")] = true
		case facts.PackageRegistryPackageVersionFactKind:
			want["version:"+replayPayloadString(envelope.Payload, "version_id")] = true
		case facts.OCIImageManifestFactKind:
			want["digest:"+replayPayloadString(envelope.Payload, "digest")] = true
		}
	}
	for endpoint, present := range want {
		if !present {
			t.Errorf("generation 2 missing retained endpoint fact %s", endpoint)
		}
	}
}

func replayPayloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
