// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// Minimal local stubs for the AWS cloud-runtime-drift adapter interfaces, used
// only by this package's own DefaultHandlers registration-gate tests
// (defaults_test.go, defaults_aws_cloud_runtime_drift_fencing_token_test.go).
// The family's own behavioral stubs moved to [awscloud] with the rest of the
// family (issue #6061); Go test files cannot share unexported symbols across
// a package boundary, so these cross-family wiring tests -- which only assert
// on DefaultHandlers/implementedDefaultDomainDefinitions, never on Handle
// behavior -- keep their own trivial doubles rather than reaching into
// awscloud's test files.

type stubAWSCloudRuntimeDriftEvidenceLoader struct{}

func (stubAWSCloudRuntimeDriftEvidenceLoader) LoadAWSCloudRuntimeDriftEvidence(
	context.Context,
	string,
	string,
) ([]cloudruntime.AddressedRow, error) {
	return nil, nil
}

type stubAWSCloudRuntimeDriftFindingWriter struct{}

func (stubAWSCloudRuntimeDriftFindingWriter) WriteAWSCloudRuntimeDriftFindings(
	context.Context,
	AWSCloudRuntimeDriftWrite,
) (AWSCloudRuntimeDriftWriteResult, error) {
	return AWSCloudRuntimeDriftWriteResult{}, nil
}

type stubAWSCloudRuntimeDriftFencingTokenIssuer struct {
	tokens []int64
	calls  int
}

func (s *stubAWSCloudRuntimeDriftFencingTokenIssuer) NextAWSCloudRuntimeDriftFencingToken(
	context.Context,
) (int64, error) {
	if len(s.tokens) == 0 {
		return 0, nil
	}
	token := s.tokens[s.calls%len(s.tokens)]
	s.calls++
	return token, nil
}
