package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestLocalAuthoritativeDeadCodeSyntheticEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("local-authoritative perf smoke is skipped in short mode")
	}
	if !perfGateEnabled(localAuthoritativePerfGateEnv) {
		t.Skipf("set %s=true to run the local-authoritative query perf smoke", localAuthoritativePerfGateEnv)
	}
	if strings.TrimSpace(os.Getenv("ESHU_NORNICDB_BINARY")) == "" {
		t.Skip("set ESHU_NORNICDB_BINARY to run the local-authoritative query perf smoke")
	}
	if runtime.GOOS == "windows" {
		t.Skip("local-authoritative query perf smoke is Unix-only in this slice")
	}

	t.Setenv("ESHU_QUERY_PROFILE", string(query.ProfileLocalAuthoritative))
	t.Setenv("ESHU_GRAPH_BACKEND", string(query.GraphBackendNornicDB))
	t.Setenv("ESHU_HOME", t.TempDir())

	workspaceRoot := t.TempDir()
	layout, err := eshulocal.BuildLayout(os.Getenv, os.UserHomeDir, runtime.GOOS, workspaceRoot)
	if err != nil {
		t.Fatalf("BuildLayout() error = %v, want nil", err)
	}

	p95, err := measureLocalAuthoritativeDeadCodeLatency(layout)
	if err != nil {
		t.Fatalf("measureLocalAuthoritativeDeadCodeLatency() error = %v, want nil", err)
	}
	t.Logf("local_authoritative synthetic dead-code p95 = %s", p95)
	if p95 > 10*time.Second {
		t.Fatalf("local_authoritative synthetic dead-code p95 = %s, want <= %s", p95, 10*time.Second)
	}
}

func measureLocalAuthoritativeDeadCodeLatency(layout eshulocal.Layout) (time.Duration, error) {
	originalStartChild := localHostStartChildProcess
	originalWaitManagedChildren := localHostWaitManagedChildren
	defer func() {
		localHostStartChildProcess = originalStartChild
		localHostWaitManagedChildren = originalWaitManagedChildren
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var measured time.Duration
	localHostStartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		if name == "eshu-reducer" {
			return &exec.Cmd{}, nil
		}
		if name != "eshu-ingester" {
			return nil, fmt.Errorf("unexpected child process %q", name)
		}

		record, err := eshulocal.ReadOwnerRecord(layout.OwnerRecordPath)
		if err != nil {
			return nil, fmt.Errorf("read owner record during dead-code perf startup: %w", err)
		}
		measured, err = runLocalAuthoritativeDeadCodeProbe(ctx, record)
		if err != nil {
			return nil, err
		}
		return &exec.Cmd{}, nil
	}
	localHostWaitManagedChildren = func(ctx context.Context, children []localHostChild, allowCleanExit string) error {
		return nil
	}

	if err := runOwnedLocalHostWithLayout(ctx, layout, localHostModeWatch); err != nil {
		return 0, err
	}
	if measured <= 0 {
		return 0, fmt.Errorf("local-authoritative query perf smoke never measured dead-code latency")
	}
	return measured, nil
}

func runLocalAuthoritativeDeadCodeProbe(ctx context.Context, record eshulocal.OwnerRecord) (time.Duration, error) {
	driver, err := openLocalAuthoritativePerfDriver(ctx, record)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = driver.Close(context.Background())
	}()

	expectedNames, err := seedSyntheticDeadCodeGraph(ctx, driver)
	if err != nil {
		return 0, err
	}
	if err := assertSyntheticDeadCodeSeedVisible(ctx, driver, expectedNames); err != nil {
		return 0, err
	}

	reader := query.NewNeo4jReader(driver, localNornicDBDefaultDatabase)
	handler := &query.CodeHandler{
		GraphBackend: query.GraphBackendNornicDB,
		Neo4j:        reader,
		Profile:      query.ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	durations := make([]time.Duration, 0, 7)
	for i := 0; i < 7; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code", bytes.NewBufferString(`{"limit":10}`))
		req.Header.Set("Accept", query.EnvelopeMIMEType)
		rec := httptest.NewRecorder()

		startedAt := time.Now()
		mux.ServeHTTP(rec, req)
		durations = append(durations, time.Since(startedAt))

		if rec.Code != http.StatusOK {
			return 0, fmt.Errorf("dead-code handler status = %d body=%s", rec.Code, rec.Body.String())
		}
		if err := assertSyntheticDeadCodeEnvelope(rec.Body.Bytes(), expectedNames); err != nil {
			return 0, err
		}
	}

	return percentileDuration(durations, 0.95), nil
}

func seedSyntheticDeadCodeGraph(ctx context.Context, driver neo4jdriver.DriverWithContext) ([]string, error) {
	prefix := fmt.Sprintf("eshu-dead-code-%d", time.Now().UnixNano())
	repoID := prefix + "-repo"
	repoName := prefix + "-repo"
	files := []map[string]any{
		{
			"id":   prefix + "-main-file",
			"path": "internal/payments/main.go",
		},
		{
			"id":   prefix + "-dead-file",
			"path": "internal/payments/dead.go",
		},
	}
	rows := []map[string]any{
		{
			"file_id":    prefix + "-main-file",
			"id":         prefix + "-main",
			"name":       "main",
			"start_line": 1,
			"end_line":   3,
		},
		{
			"file_id":    prefix + "-main-file",
			"id":         prefix + "-live",
			"name":       "liveWorker",
			"start_line": 5,
			"end_line":   7,
		},
		{
			"file_id":    prefix + "-dead-file",
			"id":         prefix + "-dead-a",
			"name":       "deadHelperA",
			"start_line": 1,
			"end_line":   3,
		},
		{
			"file_id":    prefix + "-dead-file",
			"id":         prefix + "-dead-b",
			"name":       "deadHelperB",
			"start_line": 5,
			"end_line":   7,
		},
	}
	calls := []map[string]any{
		{"from": prefix + "-main", "to": prefix + "-live"},
	}
	repoFileLinks := []map[string]any{
		{"repo_id": repoID, "file_id": prefix + "-main-file"},
		{"repo_id": repoID, "file_id": prefix + "-dead-file"},
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: localNornicDBDefaultDatabase,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	runWrite := func(cypher string, params map[string]any) error {
		_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
			result, runErr := tx.Run(ctx, cypher, params)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
			return nil, nil
		})
		if err != nil {
			return fmt.Errorf("seed synthetic dead-code graph query: %w", err)
		}
		return nil
	}

	if err := runWrite(`
MERGE (r:Repository {id: $repo_id})
SET r.name = $repo_name
`, map[string]any{
		"repo_id":   repoID,
		"repo_name": repoName,
	}); err != nil {
		return nil, err
	}

	for _, file := range files {
		if err := runWrite(`
MERGE (f:File {id: $id})
SET f.relative_path = $path,
    f.language = 'go'
`, file); err != nil {
			return nil, err
		}
	}

	for _, link := range repoFileLinks {
		if err := runWrite(`
MATCH (r:Repository {id: $repo_id})
MATCH (f:File {id: $file_id})
MERGE (r)-[:REPO_CONTAINS]->(f)
`, link); err != nil {
			return nil, err
		}
	}

	for _, row := range rows {
		if err := runWrite(`
MATCH (f:File {id: $file_id})
MERGE (e:Function {id: $id})
SET e.name = $name,
    e.language = 'go',
    e.start_line = $start_line,
    e.end_line = $end_line
MERGE (f)-[:CONTAINS]->(e)
`, row); err != nil {
			return nil, err
		}
	}

	for _, call := range calls {
		if err := runWrite(`
MATCH (start:Function {id: $from})
MATCH (target:Function {id: $to})
MERGE (start)-[:CALLS]->(target)
`, call); err != nil {
			return nil, err
		}
	}

	return []string{"deadHelperA", "deadHelperB"}, nil
}

func assertSyntheticDeadCodeSeedVisible(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	expectedDeadNames []string,
) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: localNornicDBDefaultDatabase,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	rowsAny, err := session.ExecuteRead(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		namesResult, runErr := tx.Run(ctx, `
MATCH (e:Function)
RETURN e
`, nil)
		if runErr != nil {
			return nil, runErr
		}

		names := make([]string, 0)
		nodes := make([]map[string]any, 0)
		for namesResult.Next(ctx) {
			nodeAny, _ := namesResult.Record().Get("e")
			node, ok := nodeAny.(neo4jdriver.Node)
			if !ok {
				nodes = append(nodes, map[string]any{"raw_type": fmt.Sprintf("%T", nodeAny)})
				continue
			}
			nodeSummary := map[string]any{
				"element_id": node.ElementId,
				"labels":     node.Labels,
				"props":      node.Props,
			}
			nodes = append(nodes, nodeSummary)
			if name, ok := node.Props["name"].(string); ok {
				names = append(names, name)
			}
		}
		if err := namesResult.Err(); err != nil {
			return nil, err
		}

		return map[string]any{
			"function_nodes": nodes,
			"function_names": names,
		}, nil
	})
	if err != nil {
		return fmt.Errorf("query synthetic dead-code seed visibility: %w", err)
	}

	visibility, ok := rowsAny.(map[string]any)
	if !ok {
		return fmt.Errorf("synthetic dead-code seed visibility rows = %T, want map[string]any", rowsAny)
	}
	visibleNames, _ := visibility["function_names"].([]string)
	for _, want := range expectedDeadNames {
		if !slices.Contains(visibleNames, want) {
			return fmt.Errorf(
				"synthetic dead-code seed visibility function_nodes=%#v function_names=%#v, want to include %#v",
				visibility["function_nodes"],
				visibleNames,
				want,
			)
		}
	}
	return nil
}

func assertSyntheticDeadCodeEnvelope(body []byte, expectedNames []string) error {
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("decode synthetic dead-code response: %w", err)
	}

	truth, ok := resp["truth"].(map[string]any)
	if !ok {
		return fmt.Errorf("synthetic dead-code truth = %T, want map[string]any", resp["truth"])
	}
	if got, want := truth["profile"], string(query.ProfileLocalAuthoritative); got != want {
		return fmt.Errorf("synthetic dead-code truth.profile = %#v, want %#v", got, want)
	}
	if got, want := truth["capability"], "code_quality.dead_code"; got != want {
		return fmt.Errorf("synthetic dead-code truth.capability = %#v, want %#v", got, want)
	}
	if got, want := truth["basis"], string(query.TruthBasisHybrid); got != want {
		return fmt.Errorf("synthetic dead-code truth.basis = %#v, want %#v", got, want)
	}

	data, ok := resp["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("synthetic dead-code data = %T, want map[string]any", resp["data"])
	}
	if got, want := data["limit"], float64(10); got != want {
		return fmt.Errorf("synthetic dead-code data.limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], false; got != want {
		return fmt.Errorf("synthetic dead-code data.truncated = %#v, want %#v", got, want)
	}

	results, ok := data["results"].([]any)
	if !ok {
		return fmt.Errorf("synthetic dead-code data.results = %T, want []any", data["results"])
	}
	names := make([]string, 0, len(results))
	for _, raw := range results {
		item, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("synthetic dead-code result type = %T, want map[string]any body=%s", raw, string(body))
		}
		name, ok := item["name"].(string)
		if !ok {
			return fmt.Errorf("synthetic dead-code result name = %T, want string body=%s", item["name"], string(body))
		}
		names = append(names, name)
	}
	slices.Sort(names)
	wantNames := append([]string(nil), expectedNames...)
	slices.Sort(wantNames)
	if !slices.Equal(names, wantNames) {
		return fmt.Errorf("synthetic dead-code result names = %#v, want %#v body=%s", names, wantNames, string(body))
	}
	return nil
}
