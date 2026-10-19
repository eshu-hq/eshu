// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package retract_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	relationship "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/relationship"
)

// TestLiveRelationshipDeleteEffectiveness8517 is the #8517 P1 probe: every
// stale-USES retract shape the migration doc names (bare DELETE,
// RetractSingleRepoRunsOnEdgesCypher, DEFINES, DEPENDS_ON, RUNS_IN,
// DOCUMENTS, plus the cloud-USES scope-wide retract commitEdges dispatches)
// must delete a seeded edge on the pinned Neo4j backend. Per shape it seeds
// one edge, proves a bare DELETE clears it (1 -> 0), re-seeds, then proves
// the production retract statement clears it (1 -> 0, or 2 -> 1 for DEFINES
// where the keep-listed edge must survive). Every read runs through a
// settling loop to a deadline, so a stale post-write read cannot pass.
//
// The production statements run verbatim: the DEFINES cell drives
// RepositoryEdges (which issues definesCypher), and the cloud-USES cell
// drives WorkloadCloudRelationshipWriter.RetractWorkloadCloudRelationshipEdges
// (the exact method commitEdges calls). The other cells execute the exported
// Cypher constants directly.
//
// Gate: ESHU_CYPHER_BOLT_DSN (or ESHU_NEO4J_URI) must point at the pinned
// neo4j:2026-community backend, with ESHU_CYPHER_BOLT_DATABASE (or
// ESHU_NEO4J_DATABASE, default "neo4j"). Skipped otherwise. The probe uses a
// unique id prefix per run and DETACH DELETEs it afterwards, so it is safe
// against a shared backend.
func TestLiveRelationshipDeleteEffectiveness8517(t *testing.T) {
	db := openProofDriver(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("p8517-%d", time.Now().UnixNano())
	evidence := "p8517-proof"

	cleanup := func() {
		if err := db.write(ctx,
			`MATCH (n) WHERE n.id STARTS WITH $p OR n.uid STARTS WITH $p DETACH DELETE n`,
			map[string]any{"p": prefix}); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}
	t.Cleanup(cleanup)
	cleanup()

	shapes := []proofShape{
		runsOnShape(db, prefix, evidence),
		definesShape(db, prefix),
		dependsOnShape(db, prefix, evidence),
		runsInShape(db, prefix, evidence),
		documentsShape(db, prefix, evidence),
		cloudUsesShape(db, prefix),
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			cleanup()
			if err := shape.seed(ctx); err != nil {
				t.Fatalf("seed: %v", err)
			}
			assertSettledCount(t, shape, shape.wantSeed, "seeded")
			if err := shape.bare(ctx); err != nil {
				t.Fatalf("bare DELETE: %v", err)
			}
			assertSettledCount(t, shape, shape.wantBare, "after bare DELETE")
			cleanup()
			if err := shape.seed(ctx); err != nil {
				t.Fatalf("re-seed: %v", err)
			}
			assertSettledCount(t, shape, shape.wantSeed, "re-seeded")
			if err := shape.prod(ctx); err != nil {
				t.Fatalf("production retract: %v", err)
			}
			assertSettledCount(t, shape, shape.wantProd, "after production retract")
		})
	}
}

// proofShape is one retract family: how to seed it, how to count it, and the
// two deletes under test.
type proofShape struct {
	name     string
	seed     func(ctx context.Context) error
	bare     func(ctx context.Context) error
	prod     func(ctx context.Context) error
	count    func(ctx context.Context) (int64, error)
	wantSeed int64
	wantBare int64
	wantProd int64
}

func assertSettledCount(t *testing.T, shape proofShape, want int64, stage string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var got int64
	for {
		var err error
		got, err = shape.count(context.Background())
		if err != nil {
			t.Fatalf("%s: count: %v", stage, err)
		}
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: count = %d, want %d (still %d after 30s settling)", stage, got, want, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// proofDB is a minimal Bolt driver wrapper with NoAuth, matching the
// no-auth Compose pin used for proof backends.
type proofDB struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func openProofDriver(t *testing.T) *proofDB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	}
	if dsn == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN/ESHU_NEO4J_URI not set; skipping #8517 live retract proof")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	if database == "" {
		database = strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	}
	if database == "" {
		database = "neo4j"
	}
	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open bolt driver %q: %v", dsn, err)
	}
	verifyCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(verifyCtx); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("verify bolt connectivity %q: %v", dsn, err)
	}
	db := &proofDB{driver: driver, database: database}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	return db
}

func (db *proofDB) session(ctx context.Context, mode neo4jdriver.AccessMode) neo4jdriver.SessionWithContext {
	return db.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   mode,
		DatabaseName: db.database,
	})
}

func (db *proofDB) write(ctx context.Context, cypher string, params map[string]any) error {
	session := db.session(ctx, neo4jdriver.AccessModeWrite)
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (db *proofDB) count(ctx context.Context, cypher string, params map[string]any) (int64, error) {
	session := db.session(ctx, neo4jdriver.AccessModeRead)
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return 0, err
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, err
	}
	raw, _ := record.Get("c")
	count, _ := raw.(int64)
	return count, nil
}

// ExecuteCypher implements the retract.Executor port: the DEFINES cell drives
// the production RepositoryEdges path through this Bolt backend.
func (db *proofDB) ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error {
	return db.write(ctx, cypher, params)
}

// Execute implements sourcecypher.Executor: the cloud-USES cell drives the
// production writer through this Bolt backend.
func (db *proofDB) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return db.write(ctx, stmt.Cypher, stmt.Parameters)
}

func runsOnShape(db *proofDB, prefix, evidence string) proofShape {
	repo, workload, instance, platform := prefix+"-repo", prefix+"-w", prefix+"-i", prefix+"-plat"
	seedCypher := `CREATE (repo:Repository {id: $repo})
CREATE (w:Workload {id: $workload, repo_id: $repo, name: $name})
CREATE (repo)-[:DEFINES]->(w)
CREATE (i:WorkloadInstance {uid: $instance})-[:INSTANCE_OF]->(w)
CREATE (p:Platform {id: $platform})
CREATE (i)-[rel:RUNS_ON {evidence_source: $ev}]->(p)`
	params := map[string]any{
		"repo": repo, "workload": workload, "name": "checkout",
		"instance": instance, "platform": platform, "ev": evidence,
	}
	countCypher := `MATCH (:WorkloadInstance {uid: $instance})-[rel:RUNS_ON]->(:Platform)
WHERE rel.evidence_source = $ev RETURN count(rel) AS c`
	countParams := map[string]any{"instance": instance, "ev": evidence}
	return proofShape{
		name:     "RUNS_ON-single-repo",
		wantSeed: 1, wantBare: 0, wantProd: 0,
		seed: func(ctx context.Context) error { return db.write(ctx, seedCypher, params) },
		bare: func(ctx context.Context) error {
			return db.write(ctx,
				`MATCH (:WorkloadInstance {uid: $instance})-[rel:RUNS_ON]->(:Platform)
WHERE rel.evidence_source = $ev DELETE rel`, countParams)
		},
		prod: func(ctx context.Context) error {
			return db.write(ctx, sourcecypher.RetractSingleRepoRunsOnEdgesCypher,
				map[string]any{"repo_id": repo, "evidence_source": evidence})
		},
		count: func(ctx context.Context) (int64, error) { return db.count(ctx, countCypher, countParams) },
	}
}

func definesShape(db *proofDB, prefix string) proofShape {
	// The production evidence source literal from workload materialization.
	const evidence = "finalization/workloads"
	repo, keep, stale := prefix+"-repo", prefix+"-keep", prefix+"-stale"
	seedCypher := `CREATE (repo:Repository {id: $repo})
CREATE (keep:Workload {id: $keep, repo_id: $repo, name: $name})
CREATE (stale:Workload {id: $stale, repo_id: $repo, name: $name})
CREATE (repo)-[r1:DEFINES {evidence_source: $ev}]->(keep)
CREATE (repo)-[r2:DEFINES {evidence_source: $ev}]->(stale)`
	params := map[string]any{"repo": repo, "keep": keep, "stale": stale, "name": "checkout", "ev": evidence}
	countCypher := `MATCH (:Repository {id: $repo})-[rel:DEFINES]->(:Workload)
WHERE rel.evidence_source = $ev RETURN count(rel) AS c`
	countParams := map[string]any{"repo": repo, "ev": evidence}
	return proofShape{
		name:     "DEFINES",
		wantSeed: 2, wantBare: 1, wantProd: 1,
		seed: func(ctx context.Context) error { return db.write(ctx, seedCypher, params) },
		bare: func(ctx context.Context) error {
			return db.write(ctx,
				`MATCH (:Repository {id: $repo})-[rel:DEFINES]->(w:Workload)
WHERE rel.evidence_source = $ev AND w.id = $stale DELETE rel`,
				map[string]any{"repo": repo, "ev": evidence, "stale": stale})
		},
		prod: func(ctx context.Context) error {
			_, err := retract.RepositoryEdges(ctx, db, nil, 0,
				[]retract.KeepList{{RepoID: repo, WorkloadIDs: []string{keep}, EndpointIDs: []string{}}},
				evidence)
			return err
		},
		count: func(ctx context.Context) (int64, error) { return db.count(ctx, countCypher, countParams) },
	}
}

func dependsOnShape(db *proofDB, prefix, evidence string) proofShape {
	repo, source, target := prefix+"-repo", prefix+"-a", prefix+"-b"
	seedCypher := `CREATE (a:Workload {id: $source, repo_id: $repo, name: $aname})
CREATE (b:Workload {id: $target, repo_id: $repo, name: $bname})
CREATE (a)-[rel:DEPENDS_ON {evidence_source: $ev}]->(b)`
	params := map[string]any{
		"repo": repo, "source": source, "target": target,
		"aname": "checkout", "bname": "ledger", "ev": evidence,
	}
	countCypher := `MATCH (:Workload {id: $source})-[rel:DEPENDS_ON]->(:Workload {id: $target})
WHERE rel.evidence_source = $ev RETURN count(rel) AS c`
	countParams := map[string]any{"source": source, "target": target, "ev": evidence}
	return proofShape{
		name:     "DEPENDS_ON",
		wantSeed: 1, wantBare: 0, wantProd: 0,
		seed: func(ctx context.Context) error { return db.write(ctx, seedCypher, params) },
		bare: func(ctx context.Context) error {
			return db.write(ctx,
				`MATCH (:Workload {id: $source})-[rel:DEPENDS_ON]->(:Workload {id: $target})
WHERE rel.evidence_source = $ev DELETE rel`, countParams)
		},
		prod: func(ctx context.Context) error {
			return db.write(ctx, sourcecypher.RetractWorkloadDependencyEdgesCypher,
				map[string]any{"repo_ids": []string{repo}, "evidence_source": evidence})
		},
		count: func(ctx context.Context) (int64, error) { return db.count(ctx, countCypher, countParams) },
	}
}

func runsInShape(db *proofDB, prefix, evidence string) proofShape {
	repo, fn, workload := prefix+"-repo", prefix+"-fn", prefix+"-w"
	seedCypher := `CREATE (repo:Repository {id: $repo})
CREATE (w:Workload {id: $workload, repo_id: $repo, name: $name})
CREATE (repo)-[:DEFINES]->(w)
CREATE (f:Function {uid: $fn, repo_id: $repo})
CREATE (f)-[rel:RUNS_IN {evidence_source: $ev}]->(w)`
	params := map[string]any{"repo": repo, "fn": fn, "workload": workload, "name": "checkout", "ev": evidence}
	countCypher := `MATCH (:Function {uid: $fn})-[rel:RUNS_IN]->(:Workload)
WHERE rel.evidence_source = $ev RETURN count(rel) AS c`
	countParams := map[string]any{"fn": fn, "ev": evidence}
	return proofShape{
		name:     "RUNS_IN",
		wantSeed: 1, wantBare: 0, wantProd: 0,
		seed: func(ctx context.Context) error { return db.write(ctx, seedCypher, params) },
		bare: func(ctx context.Context) error {
			return db.write(ctx,
				`MATCH (:Function {uid: $fn})-[rel:RUNS_IN]->(:Workload)
WHERE rel.evidence_source = $ev DELETE rel`, countParams)
		},
		prod: func(ctx context.Context) error {
			return db.write(ctx, sourcecypher.RetractRunsInEdgesCypher,
				map[string]any{"repo_ids": []string{repo}, "evidence_source": evidence})
		},
		count: func(ctx context.Context) (int64, error) { return db.count(ctx, countCypher, countParams) },
	}
}

func documentsShape(db *proofDB, prefix, evidence string) proofShape {
	scope, section, fn := prefix+"-scope", prefix+"-section", prefix+"-fn"
	seedCypher := `CREATE (s:DocumentationSection {uid: $section, scope_id: $scope})
CREATE (f:Function {uid: $fn})
CREATE (s)-[rel:DOCUMENTS {evidence_source: $ev}]->(f)`
	params := map[string]any{"scope": scope, "section": section, "fn": fn, "ev": evidence}
	countCypher := `MATCH (:DocumentationSection {uid: $section})-[rel:DOCUMENTS]->()
WHERE rel.evidence_source = $ev RETURN count(rel) AS c`
	countParams := map[string]any{"section": section, "ev": evidence}
	return proofShape{
		name:     "DOCUMENTS",
		wantSeed: 1, wantBare: 0, wantProd: 0,
		seed: func(ctx context.Context) error { return db.write(ctx, seedCypher, params) },
		bare: func(ctx context.Context) error {
			return db.write(ctx,
				`MATCH (:DocumentationSection {uid: $section})-[rel:DOCUMENTS]->()
WHERE rel.evidence_source = $ev DELETE rel`, countParams)
		},
		prod: func(ctx context.Context) error {
			return db.write(ctx, sourcecypher.RetractDocumentationEdgesCypher,
				map[string]any{"scope_ids": []string{scope}, "evidence_source": evidence})
		},
		count: func(ctx context.Context) (int64, error) { return db.count(ctx, countCypher, countParams) },
	}
}

func cloudUsesShape(db *proofDB, prefix string) proofShape {
	// The production evidence source literal the reducer writer stamps.
	const evidence = "reducer/workload-cloud-relationship"
	scope, other, workload, instance, resource := prefix+"-scope", prefix+"-other",
		prefix+"-w", prefix+"-i", prefix+"-res"
	gen := prefix + "-gen"
	seedCypher := `CREATE (w:Workload {id: $workload, repo_id: $repo, name: $name})
CREATE (i:WorkloadInstance {uid: $instance, environment: $env})-[:INSTANCE_OF]->(w)
CREATE (r:CloudResource {uid: $resource})
CREATE (i)-[rel:USES {scope_id: $scope, evidence_source: $ev, environment: $env}]->(r)
CREATE (i)-[other:USES {scope_id: $other, evidence_source: $ev, environment: $env}]->(r)`
	params := map[string]any{
		"workload": workload, "repo": prefix + "-repo", "name": "checkout",
		"instance": instance, "env": "prod", "resource": resource,
		"scope": scope, "other": other, "ev": evidence,
	}
	countCypher := `MATCH (:WorkloadInstance {uid: $instance})-[rel:USES]->(:CloudResource)
WHERE rel.scope_id = $scope AND rel.evidence_source = $ev RETURN count(rel) AS c`
	countParams := map[string]any{"instance": instance, "scope": scope, "ev": evidence}
	otherCount := `MATCH (:WorkloadInstance {uid: $instance})-[rel:USES]->(:CloudResource)
WHERE rel.scope_id = $other AND rel.evidence_source = $ev RETURN count(rel) AS c`
	otherParams := map[string]any{"instance": instance, "other": other, "ev": evidence}
	return proofShape{
		name:     "cloud-USES-scope-wide",
		wantSeed: 1, wantBare: 0, wantProd: 0,
		seed: func(ctx context.Context) error { return db.write(ctx, seedCypher, params) },
		bare: func(ctx context.Context) error {
			return db.write(ctx,
				`MATCH (:WorkloadInstance {uid: $instance})-[rel:USES]->(:CloudResource)
WHERE rel.scope_id = $scope AND rel.evidence_source = $ev DELETE rel`, countParams)
		},
		prod: func(ctx context.Context) error {
			writer := relationship.NewWorkloadCloudRelationshipWriter(db, 0)
			if err := writer.RetractWorkloadCloudRelationshipEdges(ctx, []string{scope}, gen, evidence); err != nil {
				return err
			}
			// Scope isolation: the other scope's edge must survive the retract.
			other, err := db.count(ctx, otherCount, otherParams)
			if err != nil {
				return err
			}
			if other != 1 {
				return fmt.Errorf("other-scope USES count = %d, want 1 (retract over-deleted)", other)
			}
			return nil
		},
		count: func(ctx context.Context) (int64, error) { return db.count(ctx, countCypher, countParams) },
	}
}
