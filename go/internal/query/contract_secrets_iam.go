// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// init registers secrets/IAM trust-chain query capabilities (issue #25). The
// read model is reducer-owned and lives in Postgres, so the capability requires
// at least the local authoritative profile; the lightweight profile cannot
// serve it and returns unsupported_capability rather than a confident answer.
func init() {
	for _, capability := range []string{
		secretsIAMIdentityTrustChainsCapability,
		secretsIAMPrivilegePostureObservationsCapability,
		secretsIAMSecretAccessPathsCapability,
		secretsIAMPostureGapsCapability,
		secretsIAMPostureSummaryCapability,
	} {
		capabilityMatrix[capability] = capabilitySupport{
			LocalLightweightMax:   nil,
			LocalAuthoritativeMax: &truthExact,
			LocalFullStackMax:     &truthExact,
			ProductionMax:         &truthExact,
			RequiredProfile:       ProfileLocalAuthoritative,
		}
	}
}
