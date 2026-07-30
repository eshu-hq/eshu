// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_shape

// Real-backend, full-production-pipeline regression closing the #5623 P0
// review finding: infra_scope_grant.go's MATCHES_STATE disjunct assumes a
// TerraformStateResource carries at most one MATCHES_STATE edge. That
// assumption depended on cypher.CanonicalNodeWriter's edge-retract statement
// running every generation; before the #5623 fix
// (terraformStateMatchesConfigEdgeRetractStatements,
// go/internal/storage/cypher/tfstate_state_match_edge_retract.go) it skipped
// entirely on a delta cycle, so a resource whose resolved owning repo changed
// on a delta cycle briefly carried edges to BOTH the old and the new owner,
// and the scope predicate -- built purely from graph shape, with no
// generation awareness -- admitted it for either repo's grant.
//
// This file proves the fix end-to-end THROUGH the real write path (not a raw
// seeded fixture): it runs cypher.CanonicalNodeWriter.Write across a full
// generation then a delta-cycle ownership reassignment -- the exact multi-edge
// stale-vs-fresh construction the #5623 P0 review asked for -- and then runs
// the production infraResourceScopePredicate against the result.
package query

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// liveScopeShapeBoltExecutor adapts a neo4j session driver to
// cypher.Executor/cypher.GroupExecutor so cypher.CanonicalNodeWriter can run
// its real write path in this test, mirroring
// internal/storage/cypher's own unexported boltTestExecutor (not importable
// across packages, so re-implemented here against the same driver contract).
type liveScopeShapeBoltExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e *liveScopeShapeBoltExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (e *liveScopeShapeBoltExecutor) ExecuteGroup(ctx context.Context, stmts []cypher.Statement) error {
	for _, stmt := range stmts {
		session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeWrite,
			DatabaseName: e.database,
		})
		_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
			return nil, nil
		})
		_ = session.Close(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

// TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment
// is the #5623 P0 review's requested multi-edge/stale-vs-fresh matrix case,
// run through the REAL production write pipeline rather than a raw seeded
// fixture (the review's own criticism of the pre-existing matrix: "your
// current tests construct edges as if written by a single coherent
// generation"). It proves that after cypher.CanonicalNodeWriter processes a
// delta-cycle ownership reassignment, the scoped-token infra predicate
// (infraResourceScopePredicate, infra_scope_grant.go) reflects only the
// CURRENT owner, not the former one -- i.e. that the #5623 retract fix
// (go/internal/storage/cypher, tfstate_state_match_edge_retract.go) actually
// closes the tenant-visibility leak at the query layer this package owns.
//
// Gate: set ESHU_CYPHER_BOLT_DSN (e.g. bolt://localhost:27687) to the isolated
// NornicDB. Run with:
//
//	go test -tags live_infra_scope_shape ./internal/query \
//	  -run TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment -count=1
func TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment(t *testing.T) {
	driver, closeDriver := liveScopeShapeDriver(t)
	defer closeDriver()
	session := driver.NewSession(context.Background(), neo4jdriver.SessionConfig{DatabaseName: liveScopeShapeDatabase()})
	defer func() { _ = session.Close(context.Background()) }()

	const (
		formerOwnerRepo  = "sst5623-pipeline-repo-former-owner"
		currentOwnerRepo = "sst5623-pipeline-repo-current-owner"
		address          = "aws_instance.web"
		stateUID         = "sst5623-pipeline-state"
		scopeID          = "sst5623-pipeline-scope"
	)
	ctx := context.Background()

	cleanup := func() {
		liveScopeRun(
			t, session,
			`MATCH (n) WHERE (n:TerraformResource AND n.repo_id IN $repo_ids) OR (n:TerraformStateResource AND n.uid = $uid) DETACH DELETE n`,
			map[string]any{"repo_ids": []string{formerOwnerRepo, currentOwnerRepo}, "uid": stateUID},
		)
	}
	defer cleanup()
	cleanup()

	// Both repos declare the same address so both generations' MERGE finds a
	// real match -- a genuine "reassigned to a different owner," not an
	// "orphaned," transition.
	liveScopeRun(
		t, session,
		`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: 1})`,
		map[string]any{"repo_id": formerOwnerRepo, "name": address, "path": "envs/former/main.tf"},
	)
	liveScopeRun(
		t, session,
		`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: 1})`,
		map[string]any{"repo_id": currentOwnerRepo, "name": address, "path": "envs/current/main.tf"},
	)

	writer := cypher.NewCanonicalNodeWriter(&liveScopeShapeBoltExecutor{driver: driver, database: liveScopeShapeDatabase()}, 500, nil)

	baseRow := projector.TerraformStateResourceRow{
		UID: stateUID, Address: address, Mode: "managed", ResourceType: "aws_instance",
		Name: "web", SourceConfidence: facts.SourceConfidenceObserved, CollectorKind: "terraform_state",
		OwnershipOutcome: projector.TerraformStateOwnershipResolved,
	}

	genOneRow := baseRow
	genOneRow.OwningRepoID = formerOwnerRepo
	genOne := projector.CanonicalMaterialization{
		ScopeID:                 scopeID,
		GenerationID:            "sst5623-pipeline-gen-1",
		FirstGeneration:         true,
		TerraformStateResources: []projector.TerraformStateResourceRow{genOneRow},
	}
	if err := writer.Write(ctx, genOne); err != nil {
		t.Fatalf("Write (generation 1, full) error: %v", err)
	}

	// No read between writes: the pinned local NornicDB image can silently
	// drop a write that follows an interleaved read on the same node within
	// one test process (documented in
	// internal/storage/cypher's TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeLive
	// and TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive).
	genTwoRow := baseRow
	genTwoRow.OwningRepoID = currentOwnerRepo
	genTwo := projector.CanonicalMaterialization{
		ScopeID:                 scopeID,
		GenerationID:            "sst5623-pipeline-gen-2-delta",
		FirstGeneration:         false,
		DeltaProjection:         true,
		TerraformStateResources: []projector.TerraformStateResourceRow{genTwoRow},
	}
	if err := writer.Write(ctx, genTwo); err != nil {
		t.Fatalf("Write (generation 2, delta reassignment) error: %v", err)
	}

	sst := " AND n.uid = '" + stateUID + "'"

	accessFormer := repositoryAccessFilter{
		allowedRepositoryIDs: []string{formerOwnerRepo},
		allowed:              map[string]struct{}{formerOwnerRepo: {}},
	}
	scalarsFormer, _ := accessFormer.scopeGrantInlineScalars()
	paramsFormer := map[string]any{"allowed_repository_ids": accessFormer.allowedRepositoryIDs, "allowed_scope_ids": []string{}}
	bindScopeGrantInlineScalars(paramsFormer, scalarsFormer)
	predFormer := infraResourceScopePredicate("n", scalarsFormer) + sst
	if got := liveScopeCount(t, session, "n", "TerraformStateResource", predFormer, paramsFormer); got != 0 {
		t.Fatalf(
			"P0 LEAK NOT CLOSED: after a real delta-cycle ownership reassignment from %s to %s, the FORMER owner's scoped grant still admits the state resource (count=%d, want 0). "+
				"This means a stale MATCHES_STATE edge survived the delta cycle and infraResourceScopePredicate is exposing it to a repository that no longer owns this resource.",
			formerOwnerRepo, currentOwnerRepo, got,
		)
	}

	accessCurrent := repositoryAccessFilter{
		allowedRepositoryIDs: []string{currentOwnerRepo},
		allowed:              map[string]struct{}{currentOwnerRepo: {}},
	}
	scalarsCurrent, _ := accessCurrent.scopeGrantInlineScalars()
	paramsCurrent := map[string]any{"allowed_repository_ids": accessCurrent.allowedRepositoryIDs, "allowed_scope_ids": []string{}}
	bindScopeGrantInlineScalars(paramsCurrent, scalarsCurrent)
	predCurrent := infraResourceScopePredicate("n", scalarsCurrent) + sst
	if got := liveScopeCount(t, session, "n", "TerraformStateResource", predCurrent, paramsCurrent); got != 1 {
		t.Fatalf("current owner's grant (%s) must admit the state resource after reassignment, got count=%d, want 1", currentOwnerRepo, got)
	}
}

// TestLiveInfraScopeShapeMatchesStateFormerOwnerExcludedOnAuthoritativeNonOwner
// is the #5623 P1 review FOLLOW-UP finding's query-layer proof, the
// counterpart to TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment
// above: it proves the scoped-token infra predicate no longer authorizes the
// FORMER owner after a delta cycle where ownership resolution returns an
// AUTHORITATIVE non-owner answer (NoOwner or AmbiguousOwner), not just after
// a reassignment to a different owner. A prior fix for the resolver-hiccup
// accuracy regression excluded these two authoritative outcomes from the
// retract's uid set by mistake (they also leave OwningRepoID empty), so the
// former owner's stale MATCHES_STATE edge -- and its scoped authorization --
// survived indefinitely.
//
// Gate: set ESHU_CYPHER_BOLT_DSN (e.g. bolt://localhost:27687) to the isolated
// NornicDB. Run with:
//
//	go test -tags live_infra_scope_shape ./internal/query \
//	  -run TestLiveInfraScopeShapeMatchesStateFormerOwnerExcludedOnAuthoritativeNonOwner -count=1
func TestLiveInfraScopeShapeMatchesStateFormerOwnerExcludedOnAuthoritativeNonOwner(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome projector.TerraformStateOwnershipOutcome
	}{
		{name: "no_owner", outcome: projector.TerraformStateOwnershipNoOwner},
		{name: "ambiguous_owner", outcome: projector.TerraformStateOwnershipAmbiguousOwner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			driver, closeDriver := liveScopeShapeDriver(t)
			defer closeDriver()
			session := driver.NewSession(context.Background(), neo4jdriver.SessionConfig{DatabaseName: liveScopeShapeDatabase()})
			defer func() { _ = session.Close(context.Background()) }()

			formerOwnerRepo := "sst5623-followup-repo-former-" + tc.name
			const address = "aws_instance.web"
			stateUID := "sst5623-followup-state-" + tc.name
			scopeID := "sst5623-followup-scope-" + tc.name
			ctx := context.Background()

			cleanup := func() {
				liveScopeRun(
					t, session,
					`MATCH (n) WHERE (n:TerraformResource AND n.repo_id = $repo_id) OR (n:TerraformStateResource AND n.uid = $uid) DETACH DELETE n`,
					map[string]any{"repo_id": formerOwnerRepo, "uid": stateUID},
				)
			}
			defer cleanup()
			cleanup()

			liveScopeRun(
				t, session,
				`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: 1})`,
				map[string]any{"repo_id": formerOwnerRepo, "name": address, "path": "envs/" + tc.name + "/main.tf"},
			)

			writer := cypher.NewCanonicalNodeWriter(&liveScopeShapeBoltExecutor{driver: driver, database: liveScopeShapeDatabase()}, 500, nil)

			genOneRow := projector.TerraformStateResourceRow{
				UID: stateUID, Address: address, Mode: "managed", ResourceType: "aws_instance",
				Name: "web", SourceConfidence: facts.SourceConfidenceObserved, CollectorKind: "terraform_state",
				OwningRepoID:     formerOwnerRepo,
				OwnershipOutcome: projector.TerraformStateOwnershipResolved,
			}
			genOne := projector.CanonicalMaterialization{
				ScopeID:                 scopeID,
				GenerationID:            "sst5623-followup-gen-1-" + tc.name,
				FirstGeneration:         true,
				TerraformStateResources: []projector.TerraformStateResourceRow{genOneRow},
			}
			if err := writer.Write(ctx, genOne); err != nil {
				t.Fatalf("Write (generation 1, full) error: %v", err)
			}

			// No read between writes -- same write-loss avoidance as the
			// sibling test above.
			genTwoRow := genOneRow
			genTwoRow.OwningRepoID = ""
			genTwoRow.OwnershipOutcome = tc.outcome
			genTwo := projector.CanonicalMaterialization{
				ScopeID:                 scopeID,
				GenerationID:            "sst5623-followup-gen-2-" + tc.name,
				FirstGeneration:         false,
				DeltaProjection:         true,
				TerraformStateResources: []projector.TerraformStateResourceRow{genTwoRow},
			}
			if err := writer.Write(ctx, genTwo); err != nil {
				t.Fatalf("Write (generation 2, delta, %s) error: %v", tc.name, err)
			}

			sst := " AND n.uid = '" + stateUID + "'"
			accessFormer := repositoryAccessFilter{
				allowedRepositoryIDs: []string{formerOwnerRepo},
				allowed:              map[string]struct{}{formerOwnerRepo: {}},
			}
			scalarsFormer, _ := accessFormer.scopeGrantInlineScalars()
			paramsFormer := map[string]any{"allowed_repository_ids": accessFormer.allowedRepositoryIDs, "allowed_scope_ids": []string{}}
			bindScopeGrantInlineScalars(paramsFormer, scalarsFormer)
			predFormer := infraResourceScopePredicate("n", scalarsFormer) + sst
			if got := liveScopeCount(t, session, "n", "TerraformStateResource", predFormer, paramsFormer); got != 0 {
				t.Fatalf(
					"P1 FOLLOW-UP LEAK REPRODUCED (%s): after a delta cycle where ownership resolution authoritatively returned %s, "+
						"the FORMER owner's scoped grant still admits the state resource (count=%d, want 0). A stale MATCHES_STATE edge "+
						"survived because the retract's uid filter collapsed this authoritative outcome into TransientFailure "+
						"(#5623 P1 review follow-up finding)", tc.name, tc.name, got,
				)
			}
		})
	}
}

// liveScopeShapeDatabase returns ESHU_CYPHER_BOLT_DATABASE, defaulting to
// "nornic" -- the same default liveScopeShapeSession (infra_scope_grant_live_test.go)
// uses, kept as its own small helper here so this file does not need to
// reach into that sibling's unexported session type.
func liveScopeShapeDatabase() string {
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	if database == "" {
		database = "nornic"
	}
	return database
}

// liveScopeShapeDriver opens a driver handle to ESHU_CYPHER_BOLT_DSN, the
// same env var liveScopeShapeSession (infra_scope_grant_live_test.go) reads,
// for both this test's own read session and the writer's executor. Skips the
// test (matching liveScopeShapeSession's own behavior) when the env var is
// unset.
func liveScopeShapeDriver(t *testing.T) (neo4jdriver.DriverWithContext, func()) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping live #5623 MATCHES_STATE pipeline regression")
	}
	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("verify graph connectivity: %v", err)
	}
	return driver, func() { _ = driver.Close(context.Background()) }
}
