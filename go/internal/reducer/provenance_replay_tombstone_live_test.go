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
	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	provenanceReplayPackageRepoID   = "repository:replay-provenance-package-in"
	provenanceReplayBuildRepoID     = "repository:replay-provenance-build-in"
	provenanceReplayScopeID         = "git-repository-scope:" + provenanceReplayBuildRepoID
	provenanceReplayPackageID       = "pkg:npm/replay-provenance"
	provenanceReplayVersionID       = "pkg:npm/replay-provenance@1.0.0"
	provenanceReplayContainerDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	provenanceReplayBaseDigest      = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
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
	scope      scope.IngestionScope
	generation scope.ScopeGeneration
	facts      []facts.Envelope
}

// TestProvenanceReplayTombstoneCassetteDecisions is the credential-free half
// of the replay proof. It prevents the live graph assertions from passing on
// an empty or incorrectly-shaped cassette.
func TestProvenanceReplayTombstoneCassetteDecisions(t *testing.T) {
	t.Parallel()

	gen1, gen2 := loadProvenanceReplayGenerations(t)
	if gen1.scope.ScopeID != provenanceReplayScopeID || gen2.scope.ScopeID != provenanceReplayScopeID {
		t.Fatalf("scope ids = %q, %q, want %q", gen1.scope.ScopeID, gen2.scope.ScopeID, provenanceReplayScopeID)
	}
	if gen1.scope.PreviousGenerationExists {
		t.Fatal("generation 1 unexpectedly reports a previous generation")
	}
	if !gen2.scope.PreviousGenerationExists {
		t.Fatal("generation 2 must derive previous-generation existence from cassette order")
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
	builtFromRows, derivedFromRows, err := containerimage.ContainerImageEffectiveRowsForReplayTest(
		containerGen1, provenanceReplayBuildRepoID,
	)
	if err != nil {
		t.Fatalf("generation 1 effective container image rows: %v", err)
	}
	if len(builtFromRows) != 1 ||
		builtFromRows[0]["digest"] != provenanceReplayContainerDigest ||
		builtFromRows[0]["repository_id"] != provenanceReplayBuildRepoID {
		t.Fatalf("generation 1 BUILT_FROM rows = %#v, want one build-source row", builtFromRows)
	}
	if len(derivedFromRows) != 1 ||
		derivedFromRows[0]["digest"] != provenanceReplayContainerDigest ||
		derivedFromRows[0]["base_digest"] != provenanceReplayBaseDigest {
		t.Fatalf("generation 1 DERIVED_FROM rows = %#v, want child-to-base lineage row", derivedFromRows)
	}

	if got := reducer.BuildPackageSourceCorrelationDecisions(gen2.facts); len(got) != 0 {
		t.Fatalf("generation 2 package decisions = %#v, want none", got)
	}
	if got := reducer.BuildPackagePublicationDecisions(gen2.facts); len(got) != 0 {
		t.Fatalf("generation 2 publication decisions = %#v, want none", got)
	}
	containerGen2 := reducer.BuildContainerImageIdentityDecisions(gen2.facts)
	builtFromRows, derivedFromRows, err = containerimage.ContainerImageEffectiveRowsForReplayTest(
		containerGen2, provenanceReplayBuildRepoID,
	)
	if err != nil {
		t.Fatalf("generation 2 effective container image rows: %v", err)
	}
	if len(builtFromRows) != 0 {
		t.Fatalf("generation 2 BUILT_FROM rows = %#v, want none", builtFromRows)
	}
	if len(derivedFromRows) != 0 {
		t.Fatalf("generation 2 DERIVED_FROM rows = %#v, want none", derivedFromRows)
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
	if err := graph.EnsureSchemaWithBackendStrict(ctx, executor, nil, graph.SchemaBackendNornicDB); err != nil {
		t.Fatalf("ensure replay-tier NornicDB schema: %v", err)
	}
	cleanupProvenanceReplayGraph(ctx, t, executor)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		cleanupProvenanceReplayGraph(cleanupCtx, t, executor)
	})
	gen1, gen2 := loadProvenanceReplayGenerations(t)
	projectorRuntime := newProvenanceReplayProjectorRuntime(executor)
	writer := cypher.NewProvenanceEdgeWriter(executor, 10)
	projectProvenanceReplayCanonicalGeneration(ctx, t, projectorRuntime, gen1)
	seedProvenanceReplayLegacyEndpoints(ctx, t, executor)
	seedProvenanceReplaySurvivorEndpoints(ctx, t, executor)
	writeProvenanceReplaySurvivors(ctx, t, writer)
	projectProvenanceReplayGeneration(ctx, t, writer, gen1)
	assertProvenanceReplayGenerationOne(ctx, t, executor)
	t.Log("generation 1 canonical child/base nodes and in-scope DERIVED_FROM edge verified")

	projectProvenanceReplayCanonicalGeneration(ctx, t, projectorRuntime, gen2)
	projectProvenanceReplayGeneration(ctx, t, writer, gen2)
	assertProvenanceReplayGenerationTwo(ctx, t, executor)
	t.Log("generation 2 in-scope DERIVED_FROM retraction, endpoint survival, and out-of-scope edge survival verified")
	projectProvenanceReplayCanonicalGeneration(ctx, t, projectorRuntime, gen2)
	projectProvenanceReplayGeneration(ctx, t, writer, gen2)
	assertProvenanceReplayGenerationTwo(ctx, t, executor)
	t.Log("generation 2 idempotent replay verified")
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

type provenanceReplayEndpoint struct {
	label string
	key   string
	value string
}

func provenanceReplayGraphEndpoints() []provenanceReplayEndpoint {
	endpoints := append(provenanceReplayLegacyEndpoints(), []provenanceReplayEndpoint{
		{label: "ContainerImage", key: "digest", value: provenanceReplayContainerDigest},
		{label: "ContainerImage", key: "digest", value: provenanceReplayBaseDigest},
		{label: "OciRegistryRepository", key: "uid", value: "oci-registry://registry.example.invalid/replay/provenance"},
		{label: "OciRegistryRepository", key: "uid", value: "oci-registry://registry.example.invalid/replay/base"},
	}...)
	return append(endpoints, provenanceReplaySurvivorEndpoints()...)
}

func provenanceReplayLegacyEndpoints() []provenanceReplayEndpoint {
	return []provenanceReplayEndpoint{
		{label: "Repository", key: "id", value: provenanceReplayPackageRepoID},
		{label: "Package", key: "uid", value: provenanceReplayPackageID},
		{label: "PackageVersion", key: "uid", value: provenanceReplayVersionID},
		{label: "Repository", key: "id", value: provenanceReplayBuildRepoID},
	}
}

func provenanceReplaySurvivorEndpoints() []provenanceReplayEndpoint {
	return []provenanceReplayEndpoint{
		{label: "Repository", key: "id", value: provenanceReplayOutPackageRepo},
		{label: "PackageVersion", key: "uid", value: provenanceReplayOutVersionID},
		{label: "Repository", key: "id", value: provenanceReplayOutBuildRepo},
		{label: "ContainerImage", key: "digest", value: provenanceReplayOutDigest},
	}
}

func seedProvenanceReplayLegacyEndpoints(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	// Preserve #5712's PUBLISHES/BUILT_FROM endpoint setup independently of the
	// cassette's first-repository ordering. Do not seed either ContainerImage:
	// #6258 requires the production canonical projector to create both images.
	for _, endpoint := range provenanceReplayLegacyEndpoints() {
		query := fmt.Sprintf("MERGE (node:%s {%s: $value})", endpoint.label, endpoint.key)
		if err := executor.Execute(ctx, cypher.Statement{
			Cypher: query, Parameters: map[string]any{"value": endpoint.value},
		}); err != nil {
			t.Fatalf("seed provenance replay legacy endpoint: %v", err)
		}
	}
}

func seedProvenanceReplaySurvivorEndpoints(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	for _, endpoint := range provenanceReplaySurvivorEndpoints() {
		query := fmt.Sprintf("MERGE (node:%s {%s: $value})", endpoint.label, endpoint.key)
		if err := executor.Execute(ctx, cypher.Statement{
			Cypher: query, Parameters: map[string]any{"value": endpoint.value},
		}); err != nil {
			t.Fatalf("seed provenance replay survivor endpoint: %v", err)
		}
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
	if err := writer.WriteDerivedFromEdges(ctx, []map[string]any{{
		"digest":            provenanceReplayContainerDigest,
		"base_digest":       provenanceReplayBaseDigest,
		"attribution_basis": "repository_single_base",
	}}, provenanceReplayOutScopeID, "replay-provenance-out-gen1", "reducer/container-image-base-image"); err != nil {
		t.Fatalf("write out-of-scope DERIVED_FROM survivor: %v", err)
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
		ctx, writer, generation.scope.ScopeID, generation.generation.GenerationID,
		packageDecisions, publicationDecisions,
	); err != nil {
		t.Fatalf("project %s package provenance: %v", generation.generation.GenerationID, err)
	}
	containerDecisions := reducer.BuildContainerImageIdentityDecisions(generation.facts)
	if err := containerimage.ProjectEffectiveContainerImageIdentityEdgesForReplayTest(
		ctx, writer, writer, generation.scope.ScopeID, generation.generation.GenerationID, containerDecisions,
	); err != nil {
		t.Fatalf("project %s effective container provenance: %v", generation.generation.GenerationID, err)
	}
}

func assertProvenanceReplayGenerationOne(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	assertProvenanceReplayRelationship(t, "ownership PUBLISHES", readProvenanceReplayPublishes(
		ctx, t, executor, provenanceReplayPackageRepoID, "Package", "uid", provenanceReplayPackageID,
	), map[string]any{
		"scope_id": provenanceReplayScopeID, "generation_id": "replay-provenance-gen1",
		"evidence_source": "reducer/package-ownership", "evidence_kinds": "PACKAGE_OWNERSHIP_CORRELATION",
		"source_tool": "unknown",
	})
	assertProvenanceReplayRelationship(t, "publication PUBLISHES", readProvenanceReplayPublishes(
		ctx, t, executor, provenanceReplayPackageRepoID, "PackageVersion", "uid", provenanceReplayVersionID,
	), map[string]any{
		"scope_id": provenanceReplayScopeID, "generation_id": "replay-provenance-gen1",
		"evidence_source": "reducer/package-publication", "evidence_kinds": "PACKAGE_PUBLICATION_CORRELATION",
		"source_tool": "unknown",
	})
	assertProvenanceReplayRelationship(t, "BUILT_FROM", readProvenanceReplayBuiltFrom(
		ctx, t, executor, provenanceReplayContainerDigest, provenanceReplayBuildRepoID,
	), map[string]any{
		"scope_id": provenanceReplayScopeID, "generation_id": "replay-provenance-gen1",
		"evidence_source": "reducer/container-image-identity", "evidence_kinds": "CONTAINER_IMAGE_IDENTITY_EXACT_DIGEST",
		"source_tool": "oci",
	})
	assertProvenanceReplayCanonicalImages(ctx, t, executor, "replay-provenance-gen1")
	assertProvenanceReplayRelationship(t, "DERIVED_FROM", readProvenanceReplayDerivedFrom(
		ctx, t, executor, provenanceReplayContainerDigest, provenanceReplayBaseDigest, provenanceReplayScopeID,
	), map[string]any{
		"scope_id": provenanceReplayScopeID, "generation_id": "replay-provenance-gen1",
		"evidence_source": "reducer/container-image-base-image", "evidence_kinds": "CONTAINER_IMAGE_DERIVED_FROM",
		"attribution_basis": "repository_single_base", "source_tool": "oci",
	})
	assertProvenanceReplaySurvivors(ctx, t, executor)
}

func assertProvenanceReplayGenerationTwo(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
) {
	t.Helper()
	if rows := readProvenanceReplayPublishes(
		ctx, t, executor, provenanceReplayPackageRepoID, "Package", "uid", provenanceReplayPackageID,
	); len(rows) != 0 {
		t.Fatalf("generation 2 retained in-scope ownership PUBLISHES rows: %#v", rows)
	}
	if rows := readProvenanceReplayPublishes(
		ctx, t, executor, provenanceReplayPackageRepoID, "PackageVersion", "uid", provenanceReplayVersionID,
	); len(rows) != 0 {
		t.Fatalf("generation 2 retained in-scope publication PUBLISHES rows: %#v", rows)
	}
	if rows := readProvenanceReplayBuiltFrom(
		ctx, t, executor, provenanceReplayContainerDigest, provenanceReplayBuildRepoID,
	); len(rows) != 0 {
		t.Fatalf("generation 2 retained in-scope BUILT_FROM rows: %#v", rows)
	}
	if rows := readProvenanceReplayDerivedFrom(
		ctx, t, executor, provenanceReplayContainerDigest, provenanceReplayBaseDigest, provenanceReplayScopeID,
	); len(rows) != 0 {
		t.Fatalf("generation 2 retained in-scope DERIVED_FROM rows: %#v", rows)
	}
	assertProvenanceReplayNode(ctx, t, executor, "Repository", "id", provenanceReplayPackageRepoID)
	assertProvenanceReplayNode(ctx, t, executor, "Package", "uid", provenanceReplayPackageID)
	assertProvenanceReplayNode(ctx, t, executor, "PackageVersion", "uid", provenanceReplayVersionID)
	assertProvenanceReplayNode(ctx, t, executor, "Repository", "id", provenanceReplayBuildRepoID)
	assertProvenanceReplayCanonicalImages(ctx, t, executor, "replay-provenance-gen2")
	assertProvenanceReplaySurvivors(ctx, t, executor)
}

func loadProvenanceReplayGenerations(t *testing.T) (provenanceReplayGeneration, provenanceReplayGeneration) {
	t.Helper()
	source, err := cassette.NewSource(provenanceReplayCassettePath)
	if err != nil {
		t.Fatalf("load provenance replay cassette %s: %v", provenanceReplayCassettePath, err)
	}
	first := readProvenanceReplayGeneration(t, source, "generation 1")
	second := readProvenanceReplayGeneration(t, source, "generation 2")
	second.scope.PreviousGenerationExists = true
	return first, second
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
		scope:      collected.Scope,
		generation: collected.Generation,
		facts:      drainProvenanceReplayFacts(t, collected.Facts, collected.FactStreamErr),
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
		"digest:" + provenanceReplayBaseDigest:        false,
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
