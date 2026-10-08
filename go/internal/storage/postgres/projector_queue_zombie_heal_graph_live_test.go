// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// zombieHealBoltExecutor runs cypher Statements through one Bolt session per
// call against the test Neo4j backend.
type zombieHealBoltExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e *zombieHealBoltExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
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

func (e *zombieHealBoltExecutor) count(ctx context.Context, label, repoID, generationID string) (int64, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx,
		fmt.Sprintf("MATCH (n:%s) WHERE n.repo_id = $repo_id AND n.generation_id = $generation_id RETURN count(n) AS count", label),
		map[string]any{"repo_id": repoID, "generation_id": generationID})
	if err != nil {
		return 0, err
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, err
	}
	count, _ := record.Values[0].(int64)
	return count, nil
}

func (e *zombieHealBoltExecutor) cleanupRepo(ctx context.Context, repoID string) error {
	return e.Execute(ctx, cypher.Statement{
		Cypher:     "MATCH (n) WHERE n.repo_id = $repo_id OR n.id = $repo_id DETACH DELETE n",
		Parameters: map[string]any{"repo_id": repoID},
	})
}

func zombieHealBoltExecutorForTest(t *testing.T) *zombieHealBoltExecutor {
	t.Helper()
	if strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI")) == "" {
		t.Skip("set ESHU_GRAPH_BACKEND=neo4j and ESHU_NEO4J_URI/USERNAME/PASSWORD/DATABASE to run the zombie-heal graph proof")
	}
	ctx := context.Background()
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	t.Cleanup(func() {
		cc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = driver.Close(cc)
	})
	return &zombieHealBoltExecutor{driver: driver, database: cfg.DatabaseName}
}

// zombieHealFilesMat builds a file-only materialization for one generation
// of the proof repo. The repo id is nonced per run (see the test) so
// concurrent runs on a shared backend never share nodes.
func zombieHealFilesMat(repoID, generationID string, names ...string) canonical.CanonicalMaterialization {
	repoPath := "/" + repoID
	files := make([]canonical.FileRow, 0, len(names))
	for _, name := range names {
		files = append(files, canonical.FileRow{
			Path: repoPath + "/" + name, RelativePath: name, Name: name,
			Language: "go", RepoID: repoID, DirPath: repoPath,
		})
	}
	return canonical.CanonicalMaterialization{
		ScopeID:      "scope-zh",
		GenerationID: generationID,
		RepoID:       repoID,
		RepoPath:     repoPath,
		Repository:   &canonical.RepositoryRow{RepoID: repoID, Name: "zh", Path: repoPath},
		Files:        files,
	}
}

// TestProjectorZombieHealRestoresCanonicalNodesLive is the #7209 graph-truth
// acceptance on Neo4j Community: a zombie retract for a marked superseded
// generation deletes the active generation's canonical files through the
// production writer; the refused zombie Ack heals the active generation; a
// fresh worker claims the re-opened row, re-projects through the production
// writer, and Acks; the active generation's files are back and the zombie's
// are gone. Skipped unless ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN (+ its
// DISPOSABLE flag), ESHU_GRAPH_BACKEND=neo4j, and the ESHU_NEO4J_*
// connection env are set.
func TestProjectorZombieHealRestoresCanonicalNodesLive(t *testing.T) {
	database := zombieHealProofDB(t, zombieOldSeed(true))
	bolt := zombieHealBoltExecutorForTest(t)
	ctx := context.Background()
	repoID := fmt.Sprintf("repo-zh-%d", time.Now().UnixNano())

	if err := bolt.cleanupRepo(ctx, repoID); err != nil {
		t.Fatalf("clean graph fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := bolt.cleanupRepo(context.Background(), repoID); err != nil {
			t.Errorf("clean graph fixture: %v", err)
		}
	})
	writer := cypher.NewCanonicalNodeWriter(bolt, 0, nil)
	countFiles := func(generationID string) int64 {
		t.Helper()
		n, err := bolt.count(ctx, "File", repoID, generationID)
		if err != nil {
			t.Fatalf("count %s files: %v", generationID, err)
		}
		return n
	}

	// The active generation's published files, written through production.
	if err := writer.Write(ctx, zombieHealFilesMat(repoID, "gen-new", "a.go", "b.go")); err != nil {
		t.Fatalf("write gen-new files: %v", err)
	}
	if got := countFiles("gen-new"); got != 2 {
		t.Fatalf("gen-new files = %d, want 2", got)
	}

	// The zombie's post-supersede write: its retract deletes gen-new's
	// files because they carry another generation's id.
	if err := writer.Write(ctx, zombieHealFilesMat(repoID, "gen-old", "z.go")); err != nil {
		t.Fatalf("write gen-old files: %v", err)
	}
	if got := countFiles("gen-new"); got != 0 {
		t.Fatalf("gen-new files after zombie write = %d, want 0", got)
	}
	if got := countFiles("gen-old"); got != 1 {
		t.Fatalf("gen-old files after zombie write = %d, want 1", got)
	}

	// The refusal stops the zombie and heals the active generation.
	zombie := NewProjectorQueue(SQLDB{DB: database}, "zombie-worker", time.Minute)
	if err := zombie.Ack(ctx, zombieHealWork("gen-old", 1), runtime.Result{}); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("zombie Ack = %v, want ErrWorkSuperseded", err)
	}
	if state := readZombieHealRowState(t, database, "projector_scope-zh_gen-old"); state.status != "pending" {
		t.Fatalf("active row = %s, want pending", state.status)
	}

	// A fresh worker drains the re-opened row through the production
	// claim, mark, write, and ack path.
	healer := NewProjectorQueue(SQLDB{DB: database}, "heal-worker", time.Minute)
	work, ok, err := healer.Claim(ctx)
	if err != nil {
		t.Fatalf("claim re-opened row: %v", err)
	}
	if !ok || work.Generation.GenerationID != "gen-new" {
		t.Fatalf("claimed = %+v, ok=%v, want gen-new", work.Generation, ok)
	}
	if err := healer.MarkProjectionWriteStarted(ctx, work); err != nil {
		t.Fatalf("mark healed write: %v", err)
	}
	if err := writer.Write(ctx, zombieHealFilesMat(repoID, "gen-new", "a.go", "b.go")); err != nil {
		t.Fatalf("re-project gen-new: %v", err)
	}
	if err := healer.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("ack healed generation: %v", err)
	}

	if got := countFiles("gen-new"); got != 2 {
		t.Fatalf("gen-new files after heal = %d, want 2", got)
	}
	if got := countFiles("gen-old"); got != 0 {
		t.Fatalf("gen-old files after heal = %d, want 0", got)
	}
	if state := readZombieHealRowState(t, database, "projector_scope-zh_gen-old"); state.status != "succeeded" {
		t.Fatalf("active row = %s, want succeeded", state.status)
	}
}
