// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"context"
	"errors"
	"testing"
)

type scriptedRowReader struct {
	row    map[string]any
	err    error
	cypher string
	params map[string]any
}

func (s *scriptedRowReader) RunSingle(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	s.cypher, s.params = cypher, params
	return s.row, s.err
}

func TestReaderCensusRunsTheCensusStatement(t *testing.T) {
	reader := &scriptedRowReader{row: map[string]any{
		"id_bearing": int64(10), "via_id": int64(2), "via_uid_only": int64(7), "residual": int64(1),
	}}
	got, err := ReaderCensus{Reader: reader}.AnchorCensus(context.Background())
	if err != nil {
		t.Fatalf("AnchorCensus: %v", err)
	}
	if got != (Census{IDBearing: 10, ViaID: 2, ViaUIDOnly: 7, Residual: 1}) {
		t.Fatalf("census = %+v", got)
	}
	if reader.cypher != CensusCypher || len(reader.params["uid_labels"].([]string)) == 0 {
		t.Fatalf("reader ran %q with %v, want CensusCypher and the schema label sets", reader.cypher, reader.params)
	}
}

func TestReaderCensusFailsClosed(t *testing.T) {
	if _, err := (ReaderCensus{Reader: &scriptedRowReader{err: errors.New("bolt down")}}).AnchorCensus(context.Background()); err == nil {
		t.Error("a reader error was swallowed")
	}
	if _, err := (ReaderCensus{Reader: &scriptedRowReader{}}).AnchorCensus(context.Background()); err == nil {
		t.Error("a missing row was read as a zero census")
	}
	// A zero census from a missing row would read as the invariant holding.
	if _, err := (ReaderCensus{Reader: &scriptedRowReader{row: map[string]any{"residual": int64(0)}}}).AnchorCensus(context.Background()); err == nil {
		t.Error("a partial row was accepted")
	}
}
