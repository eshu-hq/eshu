// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capabilities with querycontract before
// any test runs, then runs the suite.
//
// In production these capabilities are registered by root package query's
// init() in contract/capability_matrix.go and
// contract/admission_decisions.go. Root always links into the production
// binary (it owns the router), so that init() always runs there and
// production is unaffected by this file.
//
// `go test ./internal/query/evidence` never links root package query:
// this package cannot import it without an import cycle (root's
// compat_evidence.go already imports this package for the EvidenceHandler
// compatibility alias, #6642), so root's init() functions never run in this
// test binary. Without this TestMain, every handler test in this package
// fails with the capability gate's unsupported_capability 501 -- not because
// the handler is broken, but because no capability was ever registered for
// it to check against.
//
// It registers through RelationshipEvidenceSupport, CitationPacketSupport,
// and AdmissionDecisionSupport in capabilities.go -- the same constructors
// root's matrix calls -- never a copy of their fields.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: relationshipEvidenceCapability,
			Support:    RelationshipEvidenceSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: evidenceCitationCapability,
			Support:    CitationPacketSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: AdmissionDecisionCapability,
			Support:    AdmissionDecisionSupport(),
		},
	)
	os.Exit(m.Run())
}
