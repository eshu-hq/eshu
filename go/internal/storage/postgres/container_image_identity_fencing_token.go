// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
)

// containerImageIdentityFencingTokenSequence is the Postgres sequence backing
// PostgresContainerImageIdentityFencingTokenIssuer (migration
// 093_container_image_identity_fencing_token_sequence.sql).
// #nosec G101 -- sequence name, not a credential
const containerImageIdentityFencingTokenSequence = "container_image_identity_fencing_token_seq"

// containerImageIdentityNextFencingTokenQuery issues the next value from the
// shared sequence. A plain SELECT nextval(...), not an INSERT/UPDATE: the
// sequence is the ordering primitive itself, not a table row, so there is
// nothing to upsert or conflict-guard here -- Postgres already guarantees
// nextval() never returns the same value to two concurrent callers.
const containerImageIdentityNextFencingTokenQuery = `SELECT nextval('` + containerImageIdentityFencingTokenSequence + `')`

// PostgresContainerImageIdentityFencingTokenIssuer implements
// reducer.ContainerImageIdentityFencingTokenIssuer (#5874, mirroring
// PostgresAWSCloudRuntimeDriftFencingTokenIssuer) over a plain query
// connection -- no transaction needed, since a sequence's nextval() is not
// itself transactional (Postgres deliberately does not roll back sequence
// advances on transaction abort, so gaps from a failed caller are expected
// and harmless for an ordering-only value).
type PostgresContainerImageIdentityFencingTokenIssuer struct {
	DB Queryer
}

// NextContainerImageIdentityFencingToken returns the next value in issuance
// order. See container_image_identity_admission.go's
// containerImageIdentityAdmissionQuery doc comment for why this must be
// called at evidence-read time (from ContainerImageIdentityHandler.Handle),
// not at write-commit time.
func (i PostgresContainerImageIdentityFencingTokenIssuer) NextContainerImageIdentityFencingToken(
	ctx context.Context,
) (int64, error) {
	if i.DB == nil {
		return 0, errors.New("container image identity fencing token database is required")
	}

	rows, err := i.DB.QueryContext(ctx, containerImageIdentityNextFencingTokenQuery)
	if err != nil {
		return 0, fmt.Errorf("container image identity next fencing token: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("container image identity next fencing token: %w", err)
		}
		return 0, errors.New("container image identity next fencing token: missing row")
	}
	var token int64
	if err := rows.Scan(&token); err != nil {
		return 0, fmt.Errorf("container image identity next fencing token: scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("container image identity next fencing token: %w", err)
	}

	return token, nil
}
