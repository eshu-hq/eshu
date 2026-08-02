// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

// ContainerImageIdentityActivationEpoch reads the lifecycle snapshot required
// before the legacy writer's handler loads evidence.
func (w PostgresContainerImageIdentityWriter) ContainerImageIdentityActivationEpoch(
	ctx context.Context,
	scopeID string,
	generationID string,
) (int64, error) {
	if w.ActivationLookup == nil {
		return 0, fmt.Errorf("container image identity activation lookup is required")
	}
	return w.ActivationLookup.ContainerImageIdentityActivationEpoch(ctx, scopeID, generationID)
}
