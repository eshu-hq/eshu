// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
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
