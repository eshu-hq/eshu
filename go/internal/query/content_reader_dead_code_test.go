// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func TestContentReaderDeadCodeIncomingEntityIDsReadsCompletedCodeCallIntents(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"incoming_entity_id", "resolution_method"},
			rows: [][]driver.Value{
				{"content-entity:live", "scip"},
				{"content-entity:metaclass-live", "declared"},
			},
		},
	})

	reader := NewContentReader(db)
	incoming, err := reader.DeadCodeIncomingEntityIDs(
		context.Background(),
		"repository:r_payments",
		[]string{"content-entity:live", "content-entity:dead", "content-entity:metaclass-live"},
	)
	if err != nil {
		t.Fatalf("DeadCodeIncomingEntityIDs() error = %v, want nil", err)
	}

	if _, ok := incoming["content-entity:live"]; !ok {
		t.Fatalf("incoming[content-entity:live] missing, want present")
	}
	if _, ok := incoming["content-entity:metaclass-live"]; !ok {
		t.Fatalf("incoming[content-entity:metaclass-live] missing, want present")
	}
	if _, ok := incoming["content-entity:dead"]; ok {
		t.Fatalf("incoming[content-entity:dead] present, want absent")
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(recorder.queries) = %d, want %d", got, want)
	}
	query := recorder.queries[0]
	for _, want := range []string{
		"FROM shared_projection_intents",
		"projection_domain = 'code_calls'",
		"projection_domain = 'inheritance_edges'",
		"completed_at IS NOT NULL",
		"payload->>'callee_entity_id'",
		"payload->>'target_entity_id'",
		"payload->>'parent_entity_id'",
		"payload->>'resolution_method'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	for _, want := range []driver.Value{
		"repository:r_payments",
		"content-entity:live",
		"content-entity:dead",
		"content-entity:metaclass-live",
	} {
		if !driverValuesContain(recorder.args[0], want) {
			t.Fatalf("args = %#v, want value %#v", recorder.args[0], want)
		}
	}
}

func driverValuesContain(values []driver.Value, want driver.Value) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestContentReaderDeadCodeIncomingEntityIDsSentinelRunsShippedUnboundStatement(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			// The bound statement's fallback sentinel: the active run is not
			// provably complete, so it answers nothing and asks for the
			// unbound read.
			columns: []string{"incoming_entity_id", "resolution_method"},
			rows:    [][]driver.Value{{nil, nil}},
		},
		{
			columns: []string{"incoming_entity_id", "resolution_method"},
			rows:    [][]driver.Value{{"content-entity:stale-caller", "scip"}},
		},
	})

	incoming, err := NewContentReader(db).DeadCodeIncomingEntityIDs(
		context.Background(),
		"repository:r_payments",
		[]string{"content-entity:stale-caller", "content-entity:dead"},
	)
	if err != nil {
		t.Fatalf("DeadCodeIncomingEntityIDs() error = %v, want nil", err)
	}
	if _, ok := incoming["content-entity:stale-caller"]; !ok {
		t.Fatalf("incoming = %#v, want the unbound answer", incoming)
	}
	if len(incoming) != 1 {
		t.Fatalf("incoming = %#v, want exactly the unbound answer (no sentinel entry)", incoming)
	}
	if got, want := len(recorder.queries), 2; got != want {
		t.Fatalf("len(recorder.queries) = %d, want %d", got, want)
	}
	if got, want := recorder.queries[0], deadCodeIncomingBoundQuery("$2, $3"); got != want {
		t.Fatalf("first statement is not the shipped bound statement:\n%s", got)
	}
	if got, want := recorder.queries[1], deadCodeIncomingUnboundQuery("$2, $3"); got != want {
		t.Fatalf("fallback statement is not the shipped unbound statement:\n%s", got)
	}
}

// TestDeadCodeIncomingBoundQueryKeepsUnboundBranches derives the bound
// statement's edge branches from the shipped unbound statement: removing the
// pair-correlated run predicate from the bound text must leave the unbound
// branch body verbatim, so the two reads can differ only in which run they
// count. The predicate matches the (source_run_id, generation_id) PAIR; two
// independent IN lists would admit a cross-pair row.
func TestDeadCodeIncomingBoundQueryKeepsUnboundBranches(t *testing.T) {
	t.Parallel()

	const runPredicates = "\t\t\t  AND EXISTS (\n" +
		"\t\t\t      SELECT 1 FROM active_run\n" +
		"\t\t\t      WHERE active_run.source_run_id = shared_projection_intents.source_run_id\n" +
		"\t\t\t        AND active_run.generation_id = shared_projection_intents.generation_id)\n"
	bound := deadCodeIncomingBoundQuery("$2")
	unbound := deadCodeIncomingUnboundQuery("$2")

	if got, want := strings.Count(bound, runPredicates), 3; got != want {
		t.Fatalf("bound statement pair-correlated run predicates = %d, want %d (one per branch)", got, want)
	}
	for _, crossProduct := range []string{
		"source_run_id IN (SELECT source_run_id FROM active_run)",
		"generation_id IN (SELECT generation_id FROM active_run)",
	} {
		if strings.Contains(bound, crossProduct) {
			t.Fatalf("bound statement uses an independent IN list %q, which admits cross-pair rows", crossProduct)
		}
	}
	start := strings.Index(unbound, "SELECT DISTINCT")
	end := strings.Index(unbound, "AND incoming_entity_id <> ''")
	if start < 0 || end < 0 {
		t.Fatalf("unbound statement lost its branch markers:\n%s", unbound)
	}
	branchBody := unbound[start:end]
	if !strings.Contains(strings.ReplaceAll(bound, runPredicates, ""), branchBody) {
		t.Fatalf("bound statement branches diverge from the shipped unbound branches:\n%s", bound)
	}
	for _, want := range []string{
		"generation.status = 'active'",
		"NOT active.is_delta",
		// Derived from the reducer's own constants, so renaming a domain or
		// the reducer stage fails here instead of silently never matching.
		"work.stage = '" + string(recovery.StageReducer) + "'",
		"work.domain = '" + string(reducercontract.DomainCodeCallMaterialization) + "'",
		"work.domain = '" + string(reducercontract.DomainInheritanceMaterialization) + "'",
		"work.domain IN ('" + string(reducercontract.DomainCodeCallMaterialization) + "', '" +
			string(reducercontract.DomainInheritanceMaterialization) + "')",
		"work.status <> 'succeeded'",
		"pending.completed_at IS NULL",
		"SELECT NULL::text, NULL::text",
	} {
		if !strings.Contains(bound, want) {
			t.Fatalf("bound statement missing guard %q", want)
		}
	}
	for _, absent := range []string{"source_run_id", "active_run"} {
		if strings.Contains(unbound, absent) {
			t.Fatalf("unbound statement must stay today's all-generations read; found %q", absent)
		}
	}
}
