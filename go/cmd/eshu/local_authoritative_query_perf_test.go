package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestLocalAuthoritativeCallChainSyntheticEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("local-authoritative perf smoke is skipped in short mode")
	}
	if !perfGateEnabled(localAuthoritativePerfGateEnv) {
		t.Skipf("set %s=true to run the local-authoritative query perf smoke", localAuthoritativePerfGateEnv)
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

	p95, err := measureLocalAuthoritativeCallChainLatency(layout)
	if err != nil {
		t.Fatalf("measureLocalAuthoritativeCallChainLatency() error = %v, want nil", err)
	}
	t.Logf("local_authoritative synthetic call-chain p95 = %s", p95)
	if p95 > 2*time.Second {
		t.Fatalf("local_authoritative synthetic call-chain p95 = %s, want <= %s", p95, 2*time.Second)
	}
}

func TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("local-authoritative perf smoke is skipped in short mode")
	}
	if !perfGateEnabled(localAuthoritativePerfGateEnv) {
		t.Skipf("set %s=true to run the local-authoritative query perf smoke", localAuthoritativePerfGateEnv)
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

	p95, err := measureLocalAuthoritativeTransitiveCallersLatency(layout)
	if err != nil {
		t.Fatalf("measureLocalAuthoritativeTransitiveCallersLatency() error = %v, want nil", err)
	}
	t.Logf("local_authoritative synthetic transitive-callers p95 = %s", p95)
	if p95 > 2*time.Second {
		t.Fatalf("local_authoritative synthetic transitive-callers p95 = %s, want <= %s", p95, 2*time.Second)
	}
}

func measureLocalAuthoritativeCallChainLatency(layout eshulocal.Layout) (time.Duration, error) {
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
			return nil, fmt.Errorf("read owner record during query perf startup: %w", err)
		}
		measured, err = runLocalAuthoritativeCallChainProbe(ctx, record)
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
		return 0, fmt.Errorf("local-authoritative query perf smoke never measured call-chain latency")
	}
	return measured, nil
}

func measureLocalAuthoritativeTransitiveCallersLatency(layout eshulocal.Layout) (time.Duration, error) {
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
			return nil, fmt.Errorf("read owner record during query perf startup: %w", err)
		}
		measured, err = runLocalAuthoritativeTransitiveCallersProbe(ctx, record)
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
		return 0, fmt.Errorf("local-authoritative query perf smoke never measured transitive-caller latency")
	}
	return measured, nil
}

func runLocalAuthoritativeCallChainProbe(ctx context.Context, record eshulocal.OwnerRecord) (time.Duration, error) {
	driver, err := openLocalAuthoritativePerfDriver(ctx, record)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = driver.Close(context.Background())
	}()

	startName, endName, err := seedSyntheticCallChain(ctx, driver)
	if err != nil {
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
		body := fmt.Sprintf(`{"start":"%s","end":"%s","max_depth":8}`, startName, endName)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/code/call-chain", bytes.NewBufferString(body))
		req.Header.Set("Accept", query.EnvelopeMIMEType)
		rec := httptest.NewRecorder()

		startedAt := time.Now()
		mux.ServeHTTP(rec, req)
		durations = append(durations, time.Since(startedAt))

		if rec.Code != http.StatusOK {
			return 0, fmt.Errorf("call-chain handler status = %d body=%s", rec.Code, rec.Body.String())
		}
		if err := assertSyntheticCallChainEnvelope(rec.Body.Bytes(), startName, endName); err != nil {
			return 0, err
		}
	}

	return percentileDuration(durations, 0.95), nil
}

func runLocalAuthoritativeTransitiveCallersProbe(ctx context.Context, record eshulocal.OwnerRecord) (time.Duration, error) {
	driver, err := openLocalAuthoritativePerfDriver(ctx, record)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = driver.Close(context.Background())
	}()

	startName, endName, err := seedSyntheticCallChain(ctx, driver)
	if err != nil {
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
		body := fmt.Sprintf(`{"name":"%s","direction":"incoming","relationship_type":"CALLS","transitive":true,"max_depth":8}`, endName)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships", bytes.NewBufferString(body))
		req.Header.Set("Accept", query.EnvelopeMIMEType)
		rec := httptest.NewRecorder()

		startedAt := time.Now()
		mux.ServeHTTP(rec, req)
		durations = append(durations, time.Since(startedAt))

		if rec.Code != http.StatusOK {
			return 0, fmt.Errorf("transitive-callers handler status = %d body=%s", rec.Code, rec.Body.String())
		}
		if err := assertSyntheticTransitiveCallersEnvelope(rec.Body.Bytes(), endName, startName); err != nil {
			return 0, err
		}
	}

	return percentileDuration(durations, 0.95), nil
}

func openLocalAuthoritativePerfDriver(ctx context.Context, record eshulocal.OwnerRecord) (neo4jdriver.DriverWithContext, error) {
	getenv := func(key string) string {
		switch key {
		case "ESHU_GRAPH_BACKEND":
			return string(query.GraphBackendNornicDB)
		case "ESHU_NEO4J_URI":
			return fmt.Sprintf("bolt://127.0.0.1:%d", record.GraphBoltPort)
		case "ESHU_NEO4J_USERNAME":
			return record.GraphUsername
		case "ESHU_NEO4J_PASSWORD":
			return record.GraphPassword
		case "ESHU_NEO4J_DATABASE":
			return localNornicDBDefaultDatabase
		default:
			return ""
		}
	}

	driver, _, err := internalruntime.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return nil, fmt.Errorf("open local-authoritative perf driver: %w", err)
	}
	return driver, nil
}

func seedSyntheticCallChain(ctx context.Context, driver neo4jdriver.DriverWithContext) (string, string, error) {
	prefix := fmt.Sprintf("eshu-perf-%d", time.Now().UnixNano())
	nodes := []map[string]any{
		{
			"id":          prefix + "-start",
			"name":        prefix + "-start",
			"docstring":   "synthetic start",
			"method_kind": "function",
		},
		{
			"id":          prefix + "-mid-a",
			"name":        prefix + "-mid-a",
			"docstring":   "synthetic mid a",
			"method_kind": "function",
		},
		{
			"id":          prefix + "-mid-b",
			"name":        prefix + "-mid-b",
			"docstring":   "synthetic mid b",
			"method_kind": "function",
		},
		{
			"id":          prefix + "-end",
			"name":        prefix + "-end",
			"docstring":   "synthetic end",
			"method_kind": "function",
		},
	}
	edges := []map[string]any{
		{"from": prefix + "-start", "to": prefix + "-mid-a"},
		{"from": prefix + "-mid-a", "to": prefix + "-mid-b"},
		{"from": prefix + "-mid-b", "to": prefix + "-end"},
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: localNornicDBDefaultDatabase,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		result, runErr := tx.Run(ctx, `
UNWIND $nodes AS node
MERGE (f:Function {id: node.id})
SET f.name = node.name,
    f.language = 'go',
    f.docstring = node.docstring,
    f.method_kind = node.method_kind
`, map[string]any{"nodes": nodes})
		if runErr != nil {
			return nil, runErr
		}
		if _, consumeErr := result.Consume(ctx); consumeErr != nil {
			return nil, consumeErr
		}

		result, runErr = tx.Run(ctx, `
UNWIND $edges AS edge
MATCH (start:Function {id: edge.from})
MATCH (target:Function {id: edge.to})
MERGE (start)-[:CALLS]->(target)
`, map[string]any{"edges": edges})
		if runErr != nil {
			return nil, runErr
		}
		if _, consumeErr := result.Consume(ctx); consumeErr != nil {
			return nil, consumeErr
		}
		return nil, nil
	})
	if err != nil {
		return "", "", fmt.Errorf("seed synthetic call chain: %w", err)
	}

	return prefix + "-start", prefix + "-end", nil
}

func assertSyntheticCallChainEnvelope(body []byte, startName, endName string) error {
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("decode synthetic call-chain response: %w", err)
	}

	truth, ok := resp["truth"].(map[string]any)
	if !ok {
		return fmt.Errorf("synthetic call-chain truth = %T, want map[string]any", resp["truth"])
	}
	if got, want := truth["profile"], string(query.ProfileLocalAuthoritative); got != want {
		return fmt.Errorf("synthetic call-chain truth.profile = %#v, want %#v", got, want)
	}
	if got, want := truth["basis"], string(query.TruthBasisAuthoritativeGraph); got != want {
		return fmt.Errorf("synthetic call-chain truth.basis = %#v, want %#v", got, want)
	}

	data, ok := resp["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("synthetic call-chain data = %T, want map[string]any", resp["data"])
	}
	if got, want := data["start"], startName; got != want {
		return fmt.Errorf("synthetic call-chain data.start = %#v, want %#v", got, want)
	}
	if got, want := data["end"], endName; got != want {
		return fmt.Errorf("synthetic call-chain data.end = %#v, want %#v", got, want)
	}
	chains, ok := data["chains"].([]any)
	if !ok || len(chains) == 0 {
		return fmt.Errorf("synthetic call-chain data.chains = %#v, want non-empty", data["chains"])
	}
	for _, chainAny := range chains {
		chain, ok := chainAny.(map[string]any)
		if !ok {
			continue
		}
		nodes, ok := chain["chain"].([]any)
		if !ok || len(nodes) != 4 {
			continue
		}
		first, firstOK := nodes[0].(map[string]any)
		last, lastOK := nodes[len(nodes)-1].(map[string]any)
		if firstOK && lastOK && first["name"] == startName && last["name"] == endName {
			return nil
		}
	}
	return fmt.Errorf("synthetic call-chain did not include expected 4-node path body=%s", string(body))
}

func assertSyntheticTransitiveCallersEnvelope(body []byte, entityName, farthestCaller string) error {
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("decode synthetic transitive-callers response: %w", err)
	}

	truth, ok := resp["truth"].(map[string]any)
	if !ok {
		return fmt.Errorf("synthetic transitive-callers truth = %T, want map[string]any", resp["truth"])
	}
	if got, want := truth["profile"], string(query.ProfileLocalAuthoritative); got != want {
		return fmt.Errorf("synthetic transitive-callers truth.profile = %#v, want %#v", got, want)
	}
	if got, want := truth["capability"], "call_graph.transitive_callers"; got != want {
		return fmt.Errorf("synthetic transitive-callers truth.capability = %#v, want %#v", got, want)
	}

	data, ok := resp["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("synthetic transitive-callers data = %T, want map[string]any", resp["data"])
	}
	if got, want := data["name"], entityName; got != want {
		return fmt.Errorf("synthetic transitive-callers data.name = %#v, want %#v", got, want)
	}
	incoming, ok := data["incoming"].([]any)
	if !ok || len(incoming) != 3 {
		return fmt.Errorf("synthetic transitive-callers data.incoming = %#v, want 3 callers body=%s", data["incoming"], string(body))
	}
	var sawFarthest bool
	for _, item := range incoming {
		relationship, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("synthetic transitive-callers relationship type = %T, want map[string]any", item)
		}
		if relationship["source_name"] == farthestCaller {
			sawFarthest = true
			if got, want := relationship["depth"], float64(3); got != want {
				return fmt.Errorf("synthetic transitive-callers farthest depth = %#v, want %#v", got, want)
			}
		}
	}
	if !sawFarthest {
		return fmt.Errorf("synthetic transitive-callers missing farthest caller %q in body=%s", farthestCaller, string(body))
	}
	return nil
}

func percentileDuration(samples []time.Duration, percentile float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}
