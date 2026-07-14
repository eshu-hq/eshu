// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	repoDependencyIfaExclusiveLockID = "repository:ifa-repo-dependency-proof-exclusive-lock"
	repoDependencyIfaHTTPBaseURLEnv  = "ESHU_REPO_DEPENDENCY_PROOF_HTTP_URL"
)

type repoDependencyIfaIdentitySnapshot struct {
	repositoryIDs  []string
	environmentIDs []string
	artifactIDs    []string
}

type repoDependencyIfaHTTPResult struct {
	rows [][]any
}

func acquireRepoDependencyIfaExclusiveBackend(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	artifactIDs []string,
) {
	t.Helper()
	owner, err := tryAcquireRepoDependencyIfaExclusiveBackend(ctx, exec, artifactIDs)
	if err != nil {
		t.Fatalf("acquire exclusive repo-dependency proof backend: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := releaseRepoDependencyIfaExclusiveBackend(cleanupCtx, exec, owner); err != nil {
			t.Fatalf("release exclusive repo-dependency proof backend: %v", err)
		}
	})
}

func tryAcquireRepoDependencyIfaExclusiveBackend(
	ctx context.Context,
	exec liveExecutor,
	artifactIDs []string,
) (string, error) {
	return tryAcquireRepoDependencyIfaExclusiveBackendWithLockWriter(
		ctx,
		exec,
		artifactIDs,
		exec.Execute,
	)
}

func tryAcquireRepoDependencyIfaExclusiveBackendWithLockWriter(
	ctx context.Context,
	exec liveExecutor,
	artifactIDs []string,
	writeLock func(context.Context, cypher.Statement) error,
) (string, error) {
	if err := validateRepoDependencyIfaDisposableBackend(ctx, exec, artifactIDs); err != nil {
		return "", fmt.Errorf("preflight disposable graph database before lock: %w", err)
	}
	owner := fmt.Sprintf("pid-%d-%d", os.Getpid(), time.Now().UnixNano())
	if err := writeLock(ctx, cypher.Statement{
		Cypher: `MERGE (lock:Repository {id: $id})
			ON CREATE SET lock.ifa_proof_owner = $owner`,
		Parameters: map[string]any{"id": repoDependencyIfaExclusiveLockID, "owner": owner},
	}); err != nil {
		return releaseRepoDependencyIfaLockAfterAcquireFailure(
			exec,
			owner,
			fmt.Errorf("create exclusive repo-dependency proof lock: %w", err),
		)
	}
	owned, err := exec.count(
		ctx,
		`MATCH (lock:Repository {id: $id, ifa_proof_owner: $owner}) RETURN count(lock)`,
		map[string]any{"id": repoDependencyIfaExclusiveLockID, "owner": owner},
	)
	if err != nil {
		return releaseRepoDependencyIfaLockAfterAcquireFailure(
			exec,
			owner,
			fmt.Errorf("verify exclusive repo-dependency proof backend: %w", err),
		)
	}
	if owned != 1 {
		return releaseRepoDependencyIfaLockAfterAcquireFailure(
			exec,
			owner,
			fmt.Errorf(
				"repo-dependency proof backend is already owned; use an exclusive disposable database (lock %q)",
				repoDependencyIfaExclusiveLockID,
			),
		)
	}
	if err := validateRepoDependencyIfaDisposableBackend(ctx, exec, artifactIDs); err != nil {
		return releaseRepoDependencyIfaLockAfterAcquireFailure(
			exec,
			owner,
			fmt.Errorf("recheck disposable graph database after lock: %w", err),
		)
	}
	return owner, nil
}

func releaseRepoDependencyIfaLockAfterAcquireFailure(
	exec liveExecutor,
	owner string,
	cause error,
) (string, error) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := releaseRepoDependencyIfaExclusiveBackend(cleanupCtx, exec, owner); err != nil {
		return "", fmt.Errorf("%w; release proof lock: %v", cause, err)
	}
	return "", cause
}

func releaseRepoDependencyIfaExclusiveBackend(
	ctx context.Context,
	exec liveExecutor,
	owner string,
) error {
	return exec.Execute(ctx, cypher.Statement{
		Cypher: `MATCH (lock:Repository {id: $id, ifa_proof_owner: $owner})
			DETACH DELETE lock`,
		Parameters: map[string]any{"id": repoDependencyIfaExclusiveLockID, "owner": owner},
	})
}

func validateRepoDependencyIfaDisposableBackend(
	ctx context.Context,
	exec liveExecutor,
	artifactIDs []string,
) error {
	httpSnapshot, err := readRepoDependencyIfaIdentitySnapshot(ctx, exec, artifactIDs)
	if err != nil {
		return fmt.Errorf("read HTTP identity preflight: %w", err)
	}
	boltSnapshot, err := readRepoDependencyIfaBoltIdentitySnapshot(ctx, exec, artifactIDs)
	if err != nil {
		return fmt.Errorf("read Bolt identity preflight: %w", err)
	}
	snapshot := repoDependencyIfaIdentitySnapshot{
		repositoryIDs:  uniqueSortedStrings(append(httpSnapshot.repositoryIDs, boltSnapshot.repositoryIDs...)),
		environmentIDs: uniqueSortedStrings(append(httpSnapshot.environmentIDs, boltSnapshot.environmentIDs...)),
		artifactIDs:    uniqueSortedStrings(append(httpSnapshot.artifactIDs, boltSnapshot.artifactIDs...)),
	}
	unexpectedRepositories := make([]string, 0, len(snapshot.repositoryIDs))
	for _, repositoryID := range snapshot.repositoryIDs {
		if repositoryID != repoDependencyIfaExclusiveLockID {
			unexpectedRepositories = append(unexpectedRepositories, repositoryID)
		}
	}
	if len(unexpectedRepositories) == 0 && len(snapshot.environmentIDs) == 0 && len(snapshot.artifactIDs) == 0 {
		return nil
	}
	return fmt.Errorf(
		"pre-existing identities repositories=%v environments=%v evidence_artifacts=%v",
		unexpectedRepositories,
		snapshot.environmentIDs,
		snapshot.artifactIDs,
	)
}

func readRepoDependencyIfaBoltIdentitySnapshot(
	ctx context.Context,
	exec liveExecutor,
	artifactIDs []string,
) (repoDependencyIfaIdentitySnapshot, error) {
	statements := []cypher.Statement{
		{Cypher: "MATCH (node:Repository) RETURN node.id"},
		{Cypher: "MATCH (node:Environment) RETURN node.name"},
		{Cypher: "MATCH (node:EvidenceArtifact) RETURN node.id"},
	}
	for _, artifactID := range artifactIDs {
		statements = append(statements, cypher.Statement{
			Cypher:     "MATCH (node:EvidenceArtifact {id: $id}) RETURN node.id",
			Parameters: map[string]any{"id": artifactID},
		})
	}
	values := make([][]string, 3)
	for statementIndex, statement := range statements {
		session := exec.driver.NewSession(ctx, exec.sessionConfig(neo4jdriver.AccessModeRead))
		result, err := session.Run(ctx, statement.Cypher, statement.Parameters)
		if err != nil {
			_ = session.Close(ctx)
			return repoDependencyIfaIdentitySnapshot{}, fmt.Errorf("run identity statement %d: %w", statementIndex, err)
		}
		for result.Next(ctx) {
			row := result.Record().Values
			if len(row) != 1 {
				_ = session.Close(ctx)
				return repoDependencyIfaIdentitySnapshot{}, fmt.Errorf(
					"identity statement %d row width=%d, want 1",
					statementIndex,
					len(row),
				)
			}
			valueIndex := statementIndex
			if valueIndex >= len(values) {
				valueIndex = len(values) - 1
			}
			values[valueIndex] = append(values[valueIndex], strings.TrimSpace(fmt.Sprint(row[0])))
		}
		if err := result.Err(); err != nil {
			_ = session.Close(ctx)
			return repoDependencyIfaIdentitySnapshot{}, fmt.Errorf("iterate identity statement %d: %w", statementIndex, err)
		}
		if _, err := result.Consume(ctx); err != nil {
			_ = session.Close(ctx)
			return repoDependencyIfaIdentitySnapshot{}, fmt.Errorf("consume identity statement %d: %w", statementIndex, err)
		}
		if err := session.Close(ctx); err != nil {
			return repoDependencyIfaIdentitySnapshot{}, fmt.Errorf("close identity statement %d session: %w", statementIndex, err)
		}
	}
	for index := range values {
		values[index] = uniqueSortedStrings(values[index])
	}
	return repoDependencyIfaIdentitySnapshot{
		repositoryIDs:  values[0],
		environmentIDs: values[1],
		artifactIDs:    values[2],
	}, nil
}

func readRepoDependencyIfaIdentitySnapshot(
	ctx context.Context,
	exec liveExecutor,
	artifactIDs []string,
) (repoDependencyIfaIdentitySnapshot, error) {
	statements := []map[string]any{
		{"statement": "MATCH (node:Repository) RETURN node.id", "parameters": map[string]any{}},
		{"statement": "MATCH (node:Environment) RETURN node.name", "parameters": map[string]any{}},
		{"statement": "MATCH (node:EvidenceArtifact) RETURN node.id", "parameters": map[string]any{}},
	}
	for _, artifactID := range artifactIDs {
		statements = append(statements, map[string]any{
			"statement":  "MATCH (node:EvidenceArtifact {id: $id}) RETURN node.id",
			"parameters": map[string]any{"id": artifactID},
		})
	}
	results, err := runRepoDependencyIfaHTTP(ctx, exec, statements)
	if err != nil {
		return repoDependencyIfaIdentitySnapshot{}, err
	}
	if len(results) != len(statements) {
		return repoDependencyIfaIdentitySnapshot{}, fmt.Errorf(
			"identity snapshot result sets=%d, want %d",
			len(results),
			len(statements),
		)
	}
	values := make([][]string, 3)
	for resultIndex, result := range results {
		for _, row := range result.rows {
			if len(row) != 1 {
				return repoDependencyIfaIdentitySnapshot{}, fmt.Errorf("identity snapshot result %d row width=%d, want 1", resultIndex, len(row))
			}
			valueIndex := resultIndex
			if valueIndex >= len(values) {
				valueIndex = len(values) - 1
			}
			values[valueIndex] = append(values[valueIndex], strings.TrimSpace(fmt.Sprint(row[0])))
		}
	}
	for index := range values {
		values[index] = uniqueSortedStrings(values[index])
	}
	return repoDependencyIfaIdentitySnapshot{
		repositoryIDs:  values[0],
		environmentIDs: values[1],
		artifactIDs:    values[2],
	}, nil
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func runRepoDependencyIfaHTTP(
	ctx context.Context,
	exec liveExecutor,
	statements []map[string]any,
) ([]repoDependencyIfaHTTPResult, error) {
	baseURL := strings.TrimSpace(os.Getenv(repoDependencyIfaHTTPBaseURLEnv))
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", repoDependencyIfaHTTPBaseURLEnv, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("%s must be an HTTP(S) base URL without credentials", repoDependencyIfaHTTPBaseURLEnv)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/db/" + url.PathEscape(exec.database) + "/tx/commit"
	payload := map[string]any{"statements": statements}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal identity snapshot query: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build identity snapshot request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("run identity snapshot request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("identity snapshot HTTP status %d", resp.StatusCode)
	}
	var decoded struct {
		Results []struct {
			Data []struct {
				Row []any `json:"row"`
			} `json:"data"`
		} `json:"results"`
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode identity snapshot response: %w", err)
	}
	if len(decoded.Errors) != 0 {
		return nil, fmt.Errorf("identity snapshot query failed: %s: %s", decoded.Errors[0].Code, decoded.Errors[0].Message)
	}
	results := make([]repoDependencyIfaHTTPResult, len(decoded.Results))
	for resultIndex, result := range decoded.Results {
		for _, data := range result.Data {
			results[resultIndex].rows = append(results[resultIndex].rows, data.Row)
		}
	}
	return results, nil
}
