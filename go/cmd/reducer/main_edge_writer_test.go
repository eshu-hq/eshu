// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func assertSharedEdgeWriterConfig(
	t *testing.T,
	w *sourcecypher.EdgeWriter,
	wantInheritanceGroupSize int,
	wantSQLRelationshipGroupSize int,
	wantSQLRelationshipSequentialWrites bool,
) {
	t.Helper()
	if got := w.InheritanceGroupBatchSize; got != wantInheritanceGroupSize {
		t.Errorf("inheritance edge group batch size = %d, want %d", got, wantInheritanceGroupSize)
	}
	if got := w.SQLRelationshipGroupBatchSize; got != wantSQLRelationshipGroupSize {
		t.Errorf("sql relationship edge group batch size = %d, want %d", got, wantSQLRelationshipGroupSize)
	}
	if got := w.SQLRelationshipSequentialWrites; got != wantSQLRelationshipSequentialWrites {
		t.Errorf("SQL relationship sequential writes = %t, want %t", got, wantSQLRelationshipSequentialWrites)
	}
}
