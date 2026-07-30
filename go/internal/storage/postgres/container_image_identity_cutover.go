// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

const containerImageIdentityCutoverExistsQuery = `
SELECT EXISTS (
    SELECT 1
    FROM container_image_identity_cutovers
    WHERE scope_id = $1
      AND generation_id = $2
)
`

// ContainerImageIdentityCutoverStore reads durable completion markers for the
// outcome-keyed to image-reference-keyed identity transition.
type ContainerImageIdentityCutoverStore struct {
	queryer Queryer
}

// NewContainerImageIdentityCutoverStore constructs the marker lookup.
func NewContainerImageIdentityCutoverStore(
	queryer Queryer,
) ContainerImageIdentityCutoverStore {
	return ContainerImageIdentityCutoverStore{queryer: queryer}
}

// ContainerImageIdentityCutoverExists reports whether the scope generation has
// atomically completed its first new-format publication and legacy cleanup.
func (s ContainerImageIdentityCutoverStore) ContainerImageIdentityCutoverExists(
	ctx context.Context,
	scopeID string,
	generationID string,
) (bool, error) {
	if s.queryer == nil {
		return false, fmt.Errorf("container image identity cutover queryer is required")
	}
	rows, err := s.queryer.QueryContext(
		ctx,
		containerImageIdentityCutoverExistsQuery,
		scopeID,
		generationID,
	)
	if err != nil {
		return false, fmt.Errorf("query container image identity cutover: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("query container image identity cutover: %w", err)
		}
		return false, fmt.Errorf("query container image identity cutover returned no row")
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan container image identity cutover: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("query container image identity cutover: %w", err)
	}
	return exists, nil
}
