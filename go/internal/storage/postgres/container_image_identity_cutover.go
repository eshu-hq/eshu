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

const containerImageIdentityLegacyCleanupCompleteQuery = `
SELECT (
    SELECT fact_id
    FROM fact_records
    WHERE scope_id = $1
      AND generation_id = $2
      AND fact_kind = 'reducer_container_image_identity'
      AND is_tombstone = FALSE
      AND COALESCE(payload->>'identity_format', '') <> 'image_ref_v2'
    ORDER BY fact_id
    LIMIT 1
) IS NULL
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

// ContainerImageIdentityLegacyCleanupComplete reports whether no legacy-format
// identity row remains for the scope generation. The legacy-only partial index
// makes the zero case bounded; post-cutover write guards make a proven zero
// monotonic.
func (s ContainerImageIdentityCutoverStore) ContainerImageIdentityLegacyCleanupComplete(
	ctx context.Context,
	scopeID string,
	generationID string,
) (bool, error) {
	if s.queryer == nil {
		return false, fmt.Errorf("container image identity cutover queryer is required")
	}
	rows, err := s.queryer.QueryContext(
		ctx,
		containerImageIdentityLegacyCleanupCompleteQuery,
		scopeID,
		generationID,
	)
	if err != nil {
		return false, fmt.Errorf("query container image identity legacy cleanup: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("query container image identity legacy cleanup: %w", err)
		}
		return false, fmt.Errorf("query container image identity legacy cleanup returned no row")
	}
	var complete bool
	if err := rows.Scan(&complete); err != nil {
		return false, fmt.Errorf("scan container image identity legacy cleanup: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("query container image identity legacy cleanup: %w", err)
	}
	return complete, nil
}
