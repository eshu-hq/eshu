// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
)

// OpenProtectedPublishProofForTest exposes the protected-publish proof database to the
// external test package: an admin connection and a connection logged in as the
// least-privilege runtime-writer worker. The worker also gets the work-queue privileges the
// reducer login holds in production, so a pass that records its owed marker (an UPDATE of
// its own fact_work_items row) can run through it (#11327).
func OpenProtectedPublishProofForTest(t *testing.T) (context.Context, *sql.DB, *sql.DB) {
	t.Helper()
	proof := openProtectedPublishProof(t)
	if _, err := proof.admin.ExecContext(proof.ctx, `GRANT SELECT, UPDATE ON fact_work_items TO "publish_proof_worker"`); err != nil {
		t.Fatalf("grant the worker its queue privileges: %v", err)
	}
	return proof.ctx, proof.admin, proof.worker
}
