// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

const containerImageIdentityActivationEpochQuery = `
SELECT state.activation_epoch
FROM container_image_identity_scope_state AS state
JOIN ingestion_scopes AS scope
  ON scope.scope_id = state.scope_id
 AND scope.active_generation_id = state.active_generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = state.scope_id
 AND generation.generation_id = state.active_generation_id
 AND generation.status = 'active'
WHERE state.scope_id = $1
  AND state.active_generation_id = $2
`

// ContainerImageIdentityScopeStateStore reads the generation activation epoch
// that fences one reducer evidence pass against activation ABA.
type ContainerImageIdentityScopeStateStore struct {
	db Queryer
}

// NewContainerImageIdentityScopeStateStore constructs the lifecycle reader.
func NewContainerImageIdentityScopeStateStore(db Queryer) ContainerImageIdentityScopeStateStore {
	return ContainerImageIdentityScopeStateStore{db: db}
}

// ContainerImageIdentityActivationEpoch returns the exact current epoch for a
// scope generation. A missing or duplicate row is a hard lifecycle error.
func (s ContainerImageIdentityScopeStateStore) ContainerImageIdentityActivationEpoch(
	ctx context.Context,
	scopeID string,
	generationID string,
) (int64, error) {
	if s.db == nil {
		return 0, fmt.Errorf("container image identity scope-state database is required")
	}
	rows, err := s.db.QueryContext(
		ctx,
		containerImageIdentityActivationEpochQuery,
		scopeID,
		generationID,
	)
	if err != nil {
		return 0, fmt.Errorf("query container image identity activation epoch: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("read container image identity activation epoch: %w", err)
		}
		return 0, fmt.Errorf("container image identity generation is not active")
	}
	var epoch int64
	if err := rows.Scan(&epoch); err != nil {
		return 0, fmt.Errorf("scan container image identity activation epoch: %w", err)
	}
	if rows.Next() {
		return 0, fmt.Errorf("container image identity activation epoch is not unique")
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read container image identity activation epoch: %w", err)
	}
	if epoch <= 0 {
		return 0, fmt.Errorf("container image identity activation epoch is invalid")
	}
	return epoch, nil
}
