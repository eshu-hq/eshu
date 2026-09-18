// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestFileGroupProbeLive verifies the opt-in events and graph truth through the
// ingester's real managed-transaction path against a local NornicDB instance.
// Set ESHU_FILE_GROUP_PROBE_LIVE=1 and ESHU_NEO4J_URI to enable this gate.
func TestFileGroupProbeLive(t *testing.T) {
	if os.Getenv("ESHU_FILE_GROUP_PROBE_LIVE") == "" {
		t.Skip("set ESHU_FILE_GROUP_PROBE_LIVE=1 for local NornicDB proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatal(err)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
	}
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database,
	})
	defer func() { _ = session.Close(ctx) }()
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	repoID := "eshu-file-probe-" + hex.EncodeToString(suffix[:])
	dirPath := "/" + repoID
	filePath := dirPath + "/source.go"
	params := map[string]any{"repo": repoID, "dir": dirPath, "path": filePath}
	run := func(query string) {
		t.Helper()
		_, runErr := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
			result, err := tx.Run(ctx, query, params)
			if err != nil {
				return nil, err
			}
			_, err = result.Consume(ctx)
			return nil, err
		})
		if runErr != nil {
			t.Fatal(runErr)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupSession := driver.NewSession(cleanupCtx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
		defer func() { _ = cleanupSession.Close(cleanupCtx) }()
		for _, query := range []string{
			`MATCH (f:File {path: $path}) DETACH DELETE f`,
			`MATCH (d:Directory {path: $dir}) DETACH DELETE d`,
			`MATCH (r:Repository {id: $repo}) DETACH DELETE r`,
		} {
			result, cleanupErr := cleanupSession.Run(cleanupCtx, query, params)
			if cleanupErr == nil {
				_, cleanupErr = result.Consume(cleanupCtx)
			}
			if cleanupErr != nil {
				t.Errorf("cleanup: %v", cleanupErr)
			}
		}
	})
	// Seed through the managed boundary used by the File group. Immediate
	// managed reads can miss an implicit CREATE on this pinned backend.
	run(`CREATE (:Repository {id: $repo})`)
	run(`CREATE (:Directory {path: $dir})`)

	var logs, defaultLogs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&defaultLogs, nil)))
	defer slog.SetDefault(previousLogger)
	stmt := testFileProbeStatementFor(t, repoID, dirPath, filePath)
	if err := (ingesterNeo4jExecutor{
		Driver: driver, DatabaseName: database, TxTimeout: 30 * time.Second,
		ProfileFileGroups: true,
		Logger:            slog.New(slog.NewJSONHandler(&logs, nil)),
	}).ExecuteGroup(ctx, []sourcecypher.Statement{stmt}); err != nil {
		t.Fatal(err)
	}
	gotLogs := logs.String()
	if strings.Contains(defaultLogs.String(), "file graph") {
		t.Fatal("probe used the process default logger instead of the executor logger")
	}
	for _, want := range []string{
		`"template_id":"file.nested.first_generation"`,
		`"outcome":"attempt_completed"`,
		`"outcome":"succeeded"`,
		`"post_callback_duration_s":`,
		`"pipeline_phase":"projection"`,
	} {
		if !strings.Contains(gotLogs, want) {
			t.Errorf("missing %s in %s", want, gotLogs)
		}
	}
	for _, forbidden := range []string{filePath, "UNWIND", "parameters"} {
		if strings.Contains(gotLogs, forbidden) {
			t.Errorf("log exposed %q", forbidden)
		}
	}
	count := func(query string) int64 {
		t.Helper()
		result, queryErr := session.Run(ctx, query, params)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		record, queryErr := result.Single(ctx)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		value, ok := record.Get("count")
		if !ok {
			t.Fatal("query omitted count")
		}
		n, ok := value.(int64)
		if !ok {
			t.Fatalf("count type = %T, want int64", value)
		}
		return n
	}
	fileCount := count(`MATCH (f:File {path: $path}) RETURN count(f) AS count`)
	repoEdgeCount := count(`MATCH (r:Repository {id: $repo})-[:REPO_CONTAINS]->(f:File {path: $path}) RETURN count(f) AS count`)
	directoryEdgeCount := count(`MATCH (d:Directory {path: $dir})-[:CONTAINS]->(f:File {path: $path}) RETURN count(f) AS count`)
	if fileCount != 1 || repoEdgeCount != 1 || directoryEdgeCount != 1 {
		t.Fatalf("graph counts file=%d repo_edge=%d directory_edge=%d, want one each", fileCount, repoEdgeCount, directoryEdgeCount)
	}
}
