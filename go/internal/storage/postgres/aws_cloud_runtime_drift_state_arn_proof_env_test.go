// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

const (
	// awsDriftARNProofDSNEnv names the administrative postgres database the
	// live-postgres-readiness runner hands the #7666 ARN-join proofs. Each
	// test creates and drops its own isolated schema inside it.
	awsDriftARNProofDSNEnv = "ESHU_AWS_DRIFT_ARN_PROOF_DSN"
	// awsDriftARNProofDisposableEnv acknowledges that the database behind
	// awsDriftARNProofDSNEnv may be written to freely.
	awsDriftARNProofDisposableEnv = "ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE"
)

// openAWSDriftARNProofDB opens an isolated, fully migrated schema for a #7666
// ARN-join proof. The readiness runner sets only the family pair; a family
// DSN without its disposable acknowledgment fails closed. Local runs may set
// ESHU_POSTGRES_DSN directly, and the test skips when neither is set.
func openAWSDriftARNProofDB(t *testing.T, prefix, skipMessage string) (*sql.DB, context.Context) {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv(awsDriftARNProofDSNEnv)); dsn != "" {
		if os.Getenv(awsDriftARNProofDisposableEnv) != "1" {
			t.Fatalf("%s is set without %s=1", awsDriftARNProofDSNEnv, awsDriftARNProofDisposableEnv)
		}
		t.Setenv("ESHU_POSTGRES_DSN", dsn)
	}
	return openIsolatedLiveDB(t, prefix, skipMessage)
}
