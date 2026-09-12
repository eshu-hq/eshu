// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's five capabilities with querycontract
// before any test runs, then runs the suite.
//
// In production these capabilities are registered by root package query's
// contract_secrets_iam.go init(), which the #6642 Part A move that created
// this package does not touch: contract_secrets_iam.go reads the five
// IAM*Capability consts this package exports (aliased in root as the
// unexported secretsIAM*Capability spellings) and keeps compiling unedited
// through secrets_alias.go. Root always links into the production binary (it
// owns the router), so that init() always runs there and production is
// unaffected by this file.
//
// `go test ./internal/query/secrets` never links root package query: this
// package cannot import it without an import cycle (root's secrets_alias.go
// already imports this package for the Handler compatibility alias), so
// root's init() never runs in this test binary. Without this TestMain, every
// handler test in this package fails with the capability gate's
// unsupported_capability 501 -- not because the handler is broken, but
// because no capability was ever registered for it to check against. The
// support row below is copied faithfully from contract_secrets_iam.go's
// init() (all five capabilities share the same local-authoritative-and-up
// exact ceiling); it must be kept in sync if that file's row ever changes
// (the packagereg TestMain precedent carries the identical constraint).
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	exact := querycontract.TruthLevelExact
	support := querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &exact,
		LocalFullStackMax:     &exact,
		ProductionMax:         &exact,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{Capability: IAMIdentityTrustChainsCapability, Support: support},
		querycontract.CapabilityRegistration{Capability: IAMPrivilegePostureObservationsCapability, Support: support},
		querycontract.CapabilityRegistration{Capability: IAMSecretAccessPathsCapability, Support: support},
		querycontract.CapabilityRegistration{Capability: IAMPostureGapsCapability, Support: support},
		querycontract.CapabilityRegistration{Capability: IAMPostureSummaryCapability, Support: support},
	)
	os.Exit(m.Run())
}
