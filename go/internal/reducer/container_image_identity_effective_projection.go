// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

func (h ContainerImageIdentityHandler) projectEffectiveContainerImageIdentityEdges(
	ctx context.Context,
	intent Intent,
	result ContainerImageIdentityWriteResult,
) error {
	if (h.ProvenanceEdgeWriter != nil || h.DerivedFromEdgeWriter != nil) &&
		!result.effectiveProjectionPresent {
		return fmt.Errorf("container image identity writer omitted accepted effective graph projection")
	}
	if result.effectiveSupports != nil {
		if err := h.projectContainerImageBuiltFromSupportEdges(ctx, intent, result.effectiveSupports); err != nil {
			return err
		}
		return h.projectContainerImageDerivedFromSupportEdges(ctx, intent, result.effectiveSupports)
	}
	if err := h.projectContainerImageBuiltFromEdges(ctx, intent, result.effectiveDecisions); err != nil {
		return err
	}
	return h.projectContainerImageDerivedFromEdges(ctx, intent, result.effectiveDecisions)
}
