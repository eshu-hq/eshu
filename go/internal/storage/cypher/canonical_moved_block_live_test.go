// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// movedBlockCase is one label whose Neo4j key was narrower than the uid.
type movedBlockCase struct {
	label, name, relPath string
}

var movedBlockCases = []movedBlockCase{
	{label: "TerraformModule", name: "mws_parameter_cloudwatch", relPath: "shared/resources.tf"},
	{label: "HelmChart", name: "mws_parameter_cloudwatch", relPath: "chart/Chart.yaml"},
	{label: "HelmValues", name: "values", relPath: "chart/values.yaml"},
	{label: "KustomizeOverlay", name: "overlay", relPath: "overlays/prod/kustomization.yaml"},
	{label: "TerragruntConfig", name: "config", relPath: "live/prod/terragrunt.hcl"},
}

func (c movedBlockCase) repoID() string   { return "repository:r_7095_" + strings.ToLower(c.label) }
func (c movedBlockCase) repoPath() string { return "/eshu-test/7095-" + strings.ToLower(c.label) }
func (c movedBlockCase) filePath() string { return c.repoPath() + "/" + c.relPath }
func (c movedBlockCase) uid(line int) string {
	return fmt.Sprintf("content-entity:e_7095_%s_%d", strings.ToLower(c.label), line)
}

// movedBlockLegacyConstraints are the Neo4j uniqueness constraints the schema
// emitted before #7095. The test creates them first, as an already
// bootstrapped store has them, so applying the current schema must drop them.
var movedBlockLegacyConstraints = []string{
	"CREATE CONSTRAINT kustomize_unique IF NOT EXISTS FOR (ko:KustomizeOverlay) REQUIRE ko.path IS UNIQUE",
	"CREATE CONSTRAINT helm_chart_unique IF NOT EXISTS FOR (h:HelmChart) REQUIRE (h.name, h.path) IS UNIQUE",
	"CREATE CONSTRAINT helm_values_unique IF NOT EXISTS FOR (hv:HelmValues) REQUIRE hv.path IS UNIQUE",
	"CREATE CONSTRAINT tf_module_unique IF NOT EXISTS FOR (m:TerraformModule) REQUIRE (m.name, m.path) IS UNIQUE",
	"CREATE CONSTRAINT tg_config_unique IF NOT EXISTS FOR (tg:TerragruntConfig) REQUIRE tg.path IS UNIQUE",
}

// boltSchemaExecutor adapts the bolt test runner to graph.CypherExecutor so
// the test applies the production schema through graph.EnsureSchema*.
type boltSchemaExecutor struct {
	runner *boltRetractTestRunner
}

func (e boltSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	return e.runner.runCypherGroup(ctx, Statement{Cypher: stmt.Cypher, Parameters: stmt.Parameters})
}

// movedBlockMaterialization is one generation of a repository holding one
// block of c's label. Each generation carries its own uid and start line, as
// content.CanonicalEntityID does when lines are inserted above the block.
func movedBlockMaterialization(c movedBlockCase, generation string, line int, first bool) canonical.CanonicalMaterialization {
	mat := canonical.CanonicalMaterialization{
		ScopeID:         "git-repository-scope:" + c.repoID(),
		GenerationID:    generation,
		RepoID:          c.repoID(),
		RepoPath:        c.repoPath(),
		FirstGeneration: first,
		Repository: &canonical.RepositoryRow{
			RepoID: c.repoID(), Name: "7095-" + c.label, Path: c.repoPath(), LocalPath: c.repoPath(),
		},
		Files: []canonical.FileRow{{
			Path: c.filePath(), RelativePath: c.relPath, Name: c.relPath[strings.LastIndex(c.relPath, "/")+1:],
			Language: "hcl", RepoID: c.repoID(),
		}},
		Entities: []canonical.EntityRow{{
			EntityID: c.uid(line), Label: c.label, EntityName: c.name,
			FilePath: c.filePath(), RelativePath: c.relPath,
			StartLine: line, EndLine: line + 42, Language: "hcl", RepoID: c.repoID(),
		}},
	}
	if !first {
		mat.DeltaProjection = true
		mat.DeltaFilePaths = []string{c.filePath()}
	}
	return mat
}

// TestLiveNeo4jMovedCanonicalBlockKeepsOneNode is the #7095 regression. The
// canonical writer upserts a moved block's new uid before entity_retract
// deletes the prior generation's node, so a Neo4j uniqueness constraint
// narrower than the uid identity -- tf_module_unique on (name, path),
// helm_values_unique on (path) -- rejected the delta with
// ConstraintValidationFailed and dead-lettered the repository. It drives the
// real schema bootstrap over a store that already carries the legacy
// constraints, then the real CanonicalNodeWriter for both generations.
//
// Gate: ESHU_CYPHER_BOLT_DSN pointing at a Neo4j backend and
// ESHU_CYPHER_BOLT_DATABASE=neo4j; the legacy DDL is Neo4j dialect.
func TestLiveNeo4jMovedCanonicalBlockKeepsOneNode(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE")) != "neo4j" {
		t.Skip("ESHU_CYPHER_BOLT_DATABASE=neo4j required; this proof is Neo4j-only")
	}
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	// Model a pre-#7095 store: no path lookup indexes, legacy constraints.
	// Neo4j refuses a uniqueness constraint over a schema an index already
	// covers, so a rerun must drop the indexes this test's bootstrap created.
	for _, index := range []string{"kustomize_overlay_path", "helm_values_path", "terragrunt_config_path"} {
		if err := boltWriteStatement(ctx, runner, "DROP INDEX "+index+" IF EXISTS", nil); err != nil {
			t.Fatalf("drop index %s: %v", index, err)
		}
	}
	for _, statement := range movedBlockLegacyConstraints {
		if err := boltWriteStatement(ctx, runner, statement, nil); err != nil {
			t.Fatalf("seed legacy constraint %q: %v", statement, err)
		}
	}
	if err := graph.EnsureSchemaWithBackend(ctx, boltSchemaExecutor{runner: runner}, nil, graph.SchemaBackendNeo4j); err != nil {
		t.Fatalf("apply neo4j schema: %v", err)
	}
	// The bootstrap must drop the legacy constraints on an existing store.
	for _, statement := range movedBlockLegacyConstraints {
		name := strings.Fields(statement)[2]
		count, err := boltCount(ctx, runner,
			"SHOW CONSTRAINTS YIELD name WHERE name = $name RETURN count(*) AS count",
			map[string]any{"name": name})
		if err != nil {
			t.Fatalf("show constraint %s: %v", name, err)
		}
		if count != 0 {
			t.Errorf("constraint %s still exists after the schema bootstrap; want it dropped", name)
		}
	}
	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 100, nil)

	for _, c := range movedBlockCases {
		t.Run(c.label, func(t *testing.T) {
			cleanup := func() {
				_ = boltWriteStatement(ctx, runner,
					`MATCH (n) WHERE n.repo_id = $repo_id OR n.id = $repo_id DETACH DELETE n`,
					map[string]any{"repo_id": c.repoID()})
			}
			cleanup()
			t.Cleanup(cleanup)

			if err := writer.Write(ctx, movedBlockMaterialization(c, "gen-7095-1", 726, true)); err != nil {
				t.Fatalf("generation 1 write: %v", err)
			}
			if err := writer.Write(ctx, movedBlockMaterialization(c, "gen-7095-2", 755, false)); err != nil {
				t.Fatalf("delta generation moving the block from line 726 to 755: %v", err)
			}

			rows, err := runner.runCypher(ctx,
				"MATCH (n:"+c.label+") WHERE n.repo_id = $repo_id RETURN n.uid AS uid, n.line_number AS line, n.generation_id AS gen",
				map[string]any{"repo_id": c.repoID()})
			if err != nil {
				t.Fatalf("read %s nodes: %v", c.label, err)
			}
			if len(rows) != 1 {
				t.Fatalf("%s nodes after the delta: got %d (%v), want exactly 1", c.label, len(rows), rows)
			}
			uid, _ := rows[0]["uid"].(string)
			line, _ := rows[0]["line"].(int64)
			gen, _ := rows[0]["gen"].(string)
			if uid != c.uid(755) || line != 755 || gen != "gen-7095-2" {
				t.Fatalf("%s node = {uid=%q line=%d gen=%q}, want {uid=%q line=755 gen=gen-7095-2}",
					c.label, uid, line, gen, c.uid(755))
			}
		})
	}
}
