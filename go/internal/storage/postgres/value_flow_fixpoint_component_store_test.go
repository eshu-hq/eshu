package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
)

func TestValueFlowFixpointComponentSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := ValueFlowFixpointComponentSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS value_flow_fixpoint_components",
		"component_key TEXT PRIMARY KEY",
		"result JSONB NOT NULL",
		"updated_at TIMESTAMPTZ NOT NULL",
		"value_flow_fixpoint_components_updated_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("schema missing %q:\n%s", want, sql)
		}
	}
}

func TestBootstrapDefinitionsIncludeValueFlowFixpointComponents(t *testing.T) {
	t.Parallel()

	var found Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "value_flow_fixpoint_components" {
			found = def
			break
		}
	}
	if found.Name == "" {
		t.Fatal("BootstrapDefinitions missing value_flow_fixpoint_components")
	}
	if found.Path != "schema/data-plane/postgres/032_value_flow_fixpoint_components.sql" {
		t.Fatalf("Path = %q", found.Path)
	}
	if !strings.Contains(found.SQL, "CREATE TABLE IF NOT EXISTS value_flow_fixpoint_components") {
		t.Fatalf("definition SQL missing value_flow_fixpoint_components table:\n%s", found.SQL)
	}
}

func TestValueFlowFixpointComponentStoreUpsertsResults(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewValueFlowFixpointComponentStore(db)
	result := interproc.Result{Findings: []interproc.Finding{{
		SourceFunc: "repo-a\x1fpkg\x1f\x1fhandler",
		SourceKind: "http_request",
		SinkFunc:   "repo-b\x1fpkg\x1f\x1fquery",
		SinkKind:   "sql",
		Confidence: 0.6,
	}}, Overflow: 2}

	err := store.StoreValueFlowFixpointComponents(context.Background(), map[string]interproc.Result{
		"component-a": result,
	})
	if err != nil {
		t.Fatalf("StoreValueFlowFixpointComponents() error = %v", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	for _, want := range []string{
		"INSERT INTO value_flow_fixpoint_components",
		"ON CONFLICT (component_key) DO UPDATE",
		"result = EXCLUDED.result",
		"updated_at = EXCLUDED.updated_at",
	} {
		if !strings.Contains(exec.query, want) {
			t.Fatalf("upsert query missing %q:\n%s", want, exec.query)
		}
	}
	if exec.args[0] != "component-a" {
		t.Fatalf("component key arg = %#v, want component-a", exec.args[0])
	}
	var stored interproc.Result
	if err := json.Unmarshal(exec.args[1].([]byte), &stored); err != nil {
		t.Fatalf("stored result is not JSON: %v", err)
	}
	if stored.Overflow != 2 || len(stored.Findings) != 1 {
		t.Fatalf("stored result = %+v, want one finding and overflow", stored)
	}
}

func TestValueFlowFixpointComponentStoreLoadsRequestedResults(t *testing.T) {
	t.Parallel()

	result := interproc.Result{Findings: []interproc.Finding{{
		SourceFunc: "repo-a\x1fpkg\x1f\x1fhandler",
		SourceKind: "http_request",
		SinkFunc:   "repo-b\x1fpkg\x1f\x1fquery",
		SinkKind:   "sql",
		Confidence: 0.6,
	}}}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{"component-a", payload}}}}}
	store := NewValueFlowFixpointComponentStore(db)

	loaded, err := store.LoadValueFlowFixpointComponents(context.Background(), []string{"component-a", "component-b"})
	if err != nil {
		t.Fatalf("LoadValueFlowFixpointComponents() error = %v", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("loaded count = %d, want %d", got, want)
	}
	if got := loaded["component-a"]; len(got.Findings) != 1 || got.Findings[0].SinkKind != "sql" {
		t.Fatalf("loaded component = %+v, want sql finding", got)
	}
	query := db.queries[0]
	if !strings.Contains(query.query, "WHERE component_key = ANY($1)") {
		t.Fatalf("load query is not key bounded:\n%s", query.query)
	}
	if got := query.args[0]; got == nil {
		t.Fatal("load keys arg is nil, want driver array")
	}
}
