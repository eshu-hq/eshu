// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeOSPackageAdvisoryTargetReader struct {
	requests []workflow.OSPackageAdvisoryTargetFilter
	targets  []workflow.OSPackageAdvisoryTarget
	err      error
}

type fakeSBOMComponentAdvisoryTargetReader struct {
	requests []workflow.SBOMComponentAdvisoryTargetFilter
	targets  []workflow.SBOMComponentAdvisoryTarget
	err      error
}

func (f *fakeOSPackageAdvisoryTargetReader) ListOSPackageAdvisoryTargets(
	_ context.Context,
	filter workflow.OSPackageAdvisoryTargetFilter,
) ([]workflow.OSPackageAdvisoryTarget, error) {
	f.requests = append(f.requests, filter)
	if f.err != nil {
		return nil, f.err
	}
	targets := append([]workflow.OSPackageAdvisoryTarget(nil), f.targets...)
	if filter.Limit > 0 && len(targets) > filter.Limit {
		targets = targets[:filter.Limit]
	}
	return targets, nil
}

func (f *fakeSBOMComponentAdvisoryTargetReader) ListSBOMComponentAdvisoryTargets(
	_ context.Context,
	filter workflow.SBOMComponentAdvisoryTargetFilter,
) ([]workflow.SBOMComponentAdvisoryTarget, error) {
	f.requests = append(f.requests, filter)
	if f.err != nil {
		return nil, f.err
	}
	targets := append([]workflow.SBOMComponentAdvisoryTarget(nil), f.targets...)
	if filter.Limit > 0 && len(targets) > filter.Limit {
		targets = targets[:filter.Limit]
	}
	return targets, nil
}
