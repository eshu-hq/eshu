// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/multicloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/multicloudruntimedrift"
)

// stubMultiCloudRuntimeDriftEvidenceLoader and
// stubMultiCloudRuntimeDriftFindingWriter are no-op adapters used by the root
// wiring test that proves implementedDefaultDomainDefinitions registers
// DomainMultiCloudRuntimeDrift only when both adapters are present; they
// never assert on their own inputs. The family's own behavior is tested in
// internal/reducer/multicloudruntimedrift. Go test files cannot share
// unexported symbols across a package boundary, so these are local root
// copies scoped to wiring tests only (issue #6061).
type stubMultiCloudRuntimeDriftEvidenceLoader struct{}

func (stubMultiCloudRuntimeDriftEvidenceLoader) LoadMultiCloudRuntimeDriftEvidence(
	context.Context,
	string,
	string,
) ([]multicloud.Row, error) {
	return nil, nil
}

type stubMultiCloudRuntimeDriftFindingWriter struct{}

func (stubMultiCloudRuntimeDriftFindingWriter) WriteMultiCloudRuntimeDriftFindings(
	context.Context,
	multicloudruntimedrift.MultiCloudRuntimeDriftWrite,
) (multicloudruntimedrift.MultiCloudRuntimeDriftWriteResult, error) {
	return multicloudruntimedrift.MultiCloudRuntimeDriftWriteResult{}, nil
}
