// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestDocumentationWriteProofUsesCompleteProductionBatch(t *testing.T) {
	t.Parallel()

	batch := documentationWriteProofBatchForTest("shape-proof:")
	query, args, err := buildDocumentationStreamingWriteProofQuery(batch)
	if err != nil {
		t.Fatalf("build production write proof batch: %v", err)
	}
	if got := len(batch); got != factBatchSize {
		t.Fatalf("write proof batch rows = %d, want production batch size %d", got, factBatchSize)
	}
	if got, want := len(args), factBatchSize*columnsPerFactRow; got != want {
		t.Fatalf("write proof arguments = %d, want %d", got, want)
	}
	if got := strings.Count(query, "::jsonb)"); got != factBatchSize {
		t.Fatalf("write proof VALUES rows = %d, want %d", got, factBatchSize)
	}
	if !strings.Contains(query, ") VALUES ") {
		t.Fatal("write proof does not use the production multi-value VALUES encoder")
	}
	if !strings.HasPrefix(query, upsertFactBatchPrefix) || !strings.HasSuffix(query, upsertFactBatchSuffixReturningFactID) {
		t.Fatal("write proof does not use the complete returning-aware streaming statement")
	}
}
