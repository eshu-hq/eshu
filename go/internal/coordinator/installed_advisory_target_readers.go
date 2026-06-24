// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// OSPackageAdvisoryTargetReader loads active installed OS package facts that
// can bound vulnerability-intelligence target derivation.
type OSPackageAdvisoryTargetReader interface {
	ListOSPackageAdvisoryTargets(
		context.Context,
		workflow.OSPackageAdvisoryTargetFilter,
	) ([]workflow.OSPackageAdvisoryTarget, error)
}

// SBOMComponentAdvisoryTargetReader loads active attached SBOM component facts
// that can bound vulnerability-intelligence target derivation.
type SBOMComponentAdvisoryTargetReader interface {
	ListSBOMComponentAdvisoryTargets(
		context.Context,
		workflow.SBOMComponentAdvisoryTargetFilter,
	) ([]workflow.SBOMComponentAdvisoryTarget, error)
}
