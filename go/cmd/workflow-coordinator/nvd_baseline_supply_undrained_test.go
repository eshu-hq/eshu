// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

// TestNVDBaselineCheckpointReaderMapsUndrainedGenerations proves the
// cmd-level reader attaches the shared undrained-generation set to each
// stored checkpoint: the coordinator's drain loader probes those windows
// beside the checkpoint's last chunk, so the probe path and the claim
// boost read the same set (eshu-hq/eshu#11380).
func TestNVDBaselineCheckpointReaderMapsUndrainedGenerations(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{{
		sql.NullInt64{Int64: 2026, Valid: true},
		sql.NullInt64{Int64: 1767225600000000, Valid: true},
		sql.NullString{String: "", Valid: true},
		sql.NullString{String: "sha", Valid: true},
		sql.NullInt64{Int64: 0, Valid: true},
		sql.NullBool{Bool: false, Valid: true},
		sql.NullString{String: "scope-new", Valid: true},
		sql.NullString{String: "gen-new", Valid: true},
		sql.NullInt64{Int64: 0, Valid: true},
		sql.NullInt64{Int64: 0, Valid: true},
	}}}, {Data: [][]any{{
		sql.NullInt64{Int64: 2026, Valid: true},
		sql.NullString{String: "scope-old", Valid: true},
		sql.NullString{String: "gen-old", Valid: true},
	}}}}}
	got, err := nvdBaselineCheckpointReader{database: database}.
		ReadNVDBaselineCheckpoints(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ReadNVDBaselineCheckpoints() error = %v, want nil", err)
	}
	row, ok := got[2026]
	if !ok {
		t.Fatalf("ReadNVDBaselineCheckpoints() = %+v, want the 2026 row", got)
	}
	if len(row.UndrainedGenerations) != 1 ||
		row.UndrainedGenerations[0].ScopeID != "scope-old" ||
		row.UndrainedGenerations[0].GenerationID != "gen-old" {
		t.Fatalf("ReadNVDBaselineCheckpoints() undrained = %+v, want the older window scope-old/gen-old", row.UndrainedGenerations)
	}
	if len(database.Queries) != 2 {
		t.Fatalf("reader ran %d queries, want the checkpoint read plus the shared family read", len(database.Queries))
	}
}

// TestNVDBaselineCheckpointReaderPropagatesUndrainedFailure proves a failed
// family read reaches the coordinator as an error, never as a last-only
// set the probe would mistake for the whole year.
func TestNVDBaselineCheckpointReaderPropagatesUndrainedFailure(t *testing.T) {
	t.Parallel()

	probeErr := errors.New("connection reset by peer")
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{{
		sql.NullInt64{Int64: 2026, Valid: true},
		sql.NullInt64{Int64: 1767225600000000, Valid: true},
		sql.NullString{String: "", Valid: true},
		sql.NullString{String: "sha", Valid: true},
		sql.NullInt64{Int64: 0, Valid: true},
		sql.NullBool{Bool: false, Valid: true},
		sql.NullString{String: "scope-new", Valid: true},
		sql.NullString{String: "gen-new", Valid: true},
		sql.NullInt64{Int64: 0, Valid: true},
		sql.NullInt64{Int64: 0, Valid: true},
	}}}, {FailWith: probeErr}}}
	got, err := nvdBaselineCheckpointReader{database: database}.
		ReadNVDBaselineCheckpoints(context.Background(), "run-1")
	if err == nil {
		t.Fatal("ReadNVDBaselineCheckpoints() error = nil on family read failure, want an error")
	}
	if len(got) != 0 {
		t.Fatalf("ReadNVDBaselineCheckpoints() = %+v, want no rows alongside the error", got)
	}
}
