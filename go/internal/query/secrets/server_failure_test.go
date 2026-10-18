// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestSecretsIAMReadFailuresAnswerFixedText drives every secrets/IAM read
// failure step through a backend fault, a client cancel, and a stale reader
// (#7674). It swaps the package-level secretsHandlerTracer, so it must not be
// parallel.
func TestSecretsIAMReadFailuresAnswerFixedText(t *testing.T) {
	routes := []failure.Route{
		{
			Name:    "identity trust chains",
			Path:    "/api/v0/secrets-iam/identity-trust-chains?scope_id=scope-a&limit=10",
			Message: iamIdentityTrustChainsFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{IdentityTrustChains: failingSecretsStores{err: err}} },
		},
		{
			Name:    "privilege posture observations",
			Path:    "/api/v0/secrets-iam/privilege-posture-observations?scope_id=scope-a&limit=10",
			Message: iamPrivilegePostureObservationsFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{PrivilegePostureObservations: failingSecretsStores{err: err}}
			},
		},
		{
			Name:    "secret access paths",
			Path:    "/api/v0/secrets-iam/secret-access-paths?scope_id=scope-a&limit=10",
			Message: iamAccessPathsFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{SecretAccessPaths: failingSecretsStores{err: err}} },
		},
		{
			Name:    "posture gaps",
			Path:    "/api/v0/secrets-iam/posture-gaps?scope_id=scope-a&limit=10",
			Message: iamPostureGapsFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{PostureGaps: failingSecretsStores{err: err}} },
		},
		{
			Name:    "posture summary",
			Path:    "/api/v0/secrets-iam/posture-summary?scope_id=scope-a",
			Message: iamPostureSummaryFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Summary: failingSecretsStores{err: err}} },
		},
		{
			Name:    "posture summary grant section",
			Path:    "/api/v0/secrets-iam/posture-summary?scope_id=scope-a",
			Message: iamGrantPostureFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Summary: failingSecretsStores{}, GrantPosture: failingSecretsStores{err: err}}
			},
		},
	}
	for i := range routes {
		routes[i].PlainDetail = true
	}
	failure.Run(t, failure.Suite{HandlerTracer: &secretsHandlerTracer, Routes: routes})
}

// failingSecretsStores implements every secrets/IAM store port and fails each
// read with err; a nil err answers an empty result.
type failingSecretsStores struct{ err error }

func (s failingSecretsStores) ListSecretsIAMIdentityTrustChains(
	context.Context, IAMIdentityTrustChainFilter,
) ([]IAMIdentityTrustChainRow, error) {
	return nil, s.err
}

func (s failingSecretsStores) ListSecretsIAMPrivilegePostureObservations(
	context.Context, IAMPrivilegePostureObservationFilter,
) ([]IAMPrivilegePostureObservationRow, error) {
	return nil, s.err
}

func (s failingSecretsStores) ListSecretsIAMSecretAccessPaths(
	context.Context, IAMSecretAccessPathFilter,
) ([]IAMSecretAccessPathRow, error) {
	return nil, s.err
}

func (s failingSecretsStores) ListSecretsIAMPostureGaps(context.Context, IAMPostureGapFilter) ([]IAMPostureGapRow, error) {
	return nil, s.err
}

func (s failingSecretsStores) SummarizeSecretsIAMPosture(context.Context, string) (IAMPostureSummary, error) {
	return IAMPostureSummary{}, s.err
}

func (s failingSecretsStores) SummarizeS3ExternalPrincipalGrantPosture(context.Context, string) (IAMGrantPosture, error) {
	return IAMGrantPosture{}, s.err
}
