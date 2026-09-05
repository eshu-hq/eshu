// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capabilities with querycontract before
// any test runs, then runs the suite.
//
// In production these capabilities are registered by root package query's
// init() in contract_supply_chain.go. Root always links into the production
// binary (it owns the router), so that init() always runs there and
// production is unaffected by this file.
//
// `go test ./internal/query/supplychain` never links root package query:
// this package cannot import it without an import cycle (root's
// supply_chain_hub_alias.go already imports this package for the
// SupplyChainHandler compatibility alias, #6060), so root's init()
// functions never run in this test binary. Without this TestMain, every
// handler test in this package fails with the capability gate's
// unsupported_capability 501 -- not because the handler is broken, but
// because no capability was ever registered for it to check against.
//
// It registers through LightweightExactSupport and AuthoritativeExactSupport
// in capabilities.go -- the same constructors root's init calls -- never a
// copy of their fields. A copied row is what the packagereg TestMain still
// carries, under a comment asking the next editor to keep it in sync;
// nothing enforces that, and the semanticsearch TestMain already had to
// relearn the lesson when two fields flipped while its tests stayed green
// against a profile production no longer served.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: VulnerabilityScannerReadContractCapability,
			Support:    LightweightExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: SBOMAttestationAttachmentsCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: SupplyChainImpactFindingsCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: SupplyChainImpactExplanationCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: ContainerImageIdentitiesCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: SecurityAlertReconciliationsCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: SupplyChainImpactAggregateCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: SecurityAlertReconciliationAggregateCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: ContainerImageIdentityAggregateCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: SBOMAttestationAttachmentAggregateCapability,
			Support:    AuthoritativeExactSupport(),
		},
	)
	os.Exit(m.Run())
}
