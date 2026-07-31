// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
)

// awsCloudRuntimeDriftFencingTokenSequence is the Postgres sequence backing
// PostgresAWSCloudRuntimeDriftFencingTokenIssuer (migration
// 089_aws_cloud_runtime_drift_fencing_token_sequence.sql).
// #nosec G101 -- sequence name, not a credential
const awsCloudRuntimeDriftFencingTokenSequence = "aws_cloud_runtime_drift_fencing_token_seq"

// awsCloudRuntimeDriftNextFencingTokenQuery issues the next value from the
// shared sequence. A plain SELECT nextval(...), not an INSERT/UPDATE: the
// sequence is the ordering primitive itself, not a table row, so there is
// nothing to upsert or conflict-guard here -- Postgres already guarantees
// nextval() never returns the same value to two concurrent callers.
const awsCloudRuntimeDriftNextFencingTokenQuery = `SELECT nextval('` + awsCloudRuntimeDriftFencingTokenSequence + `')`

// PostgresAWSCloudRuntimeDriftFencingTokenIssuer implements
// reducer.AWSCloudRuntimeDriftFencingTokenIssuer (#5848, #5875 P1) over a
// plain query connection -- no transaction needed, since a sequence's
// nextval() is not itself transactional (Postgres deliberately does not roll
// back sequence advances on transaction abort, so gaps from a failed caller
// are expected and harmless for an ordering-only value).
type PostgresAWSCloudRuntimeDriftFencingTokenIssuer struct {
	DB Queryer
}

// NextAWSCloudRuntimeDriftFencingToken returns the next value in issuance
// order. See aws_cloud_runtime_drift_admission.go's
// AWSCloudRuntimeDriftFencingTokenIssuer doc comment for why this must be
// called at evidence-read time (from AWSCloudRuntimeDriftHandler.Handle),
// not at write-commit time.
func (i PostgresAWSCloudRuntimeDriftFencingTokenIssuer) NextAWSCloudRuntimeDriftFencingToken(
	ctx context.Context,
) (int64, error) {
	if i.DB == nil {
		return 0, errors.New("aws cloud runtime drift fencing token database is required")
	}

	rows, err := i.DB.QueryContext(ctx, awsCloudRuntimeDriftNextFencingTokenQuery)
	if err != nil {
		return 0, fmt.Errorf("aws cloud runtime drift next fencing token: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("aws cloud runtime drift next fencing token: %w", err)
		}
		return 0, errors.New("aws cloud runtime drift next fencing token: missing row")
	}
	var token int64
	if err := rows.Scan(&token); err != nil {
		return 0, fmt.Errorf("aws cloud runtime drift next fencing token: scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("aws cloud runtime drift next fencing token: %w", err)
	}

	return token, nil
}
