// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testfixtures

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// DSNForDeferredPartitionMemoProof is the disposable proof DSN, or a skip.
// It merges the identical dsnForDeferredPartitionMemoProof twins from
// ingestion_backfill_partition_memo_proof_helpers_test.go and
// activation/fixtures_test.go (#7648).
func DSNForDeferredPartitionMemoProof(t *testing.T) string {
	t.Helper()
	return postgresproof.DeferredPartitionProofDSN(t)
}
