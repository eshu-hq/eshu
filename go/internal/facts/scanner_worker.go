// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// ScannerWorkerAnalysisFactKind identifies one source fact emitted by an
	// isolated security analyzer. Reducers must convert these source facts into
	// any user-facing findings.
	ScannerWorkerAnalysisFactKind = "scanner_worker.analysis"
	// ScannerWorkerWarningFactKind identifies a non-fatal scanner-worker
	// warning such as an analyzer timeout, skipped target, or partial result.
	ScannerWorkerWarningFactKind = "scanner_worker.warning"

	// ScannerWorkerSchemaVersionV1 is the first scanner-worker source fact
	// schema.
	ScannerWorkerSchemaVersionV1 = "1.0.0"
)

var scannerWorkerFactKinds = []string{
	ScannerWorkerAnalysisFactKind,
	ScannerWorkerWarningFactKind,
}

var scannerWorkerSchemaVersions = map[string]string{
	ScannerWorkerAnalysisFactKind: ScannerWorkerSchemaVersionV1,
	ScannerWorkerWarningFactKind:  ScannerWorkerSchemaVersionV1,
}

// ScannerWorkerFactKinds returns the accepted scanner-worker source fact kinds
// in contract order.
func ScannerWorkerFactKinds() []string {
	return slices.Clone(scannerWorkerFactKinds)
}

// ScannerWorkerSchemaVersion returns the schema version for a scanner-worker
// source fact kind.
func ScannerWorkerSchemaVersion(factKind string) (string, bool) {
	version, ok := scannerWorkerSchemaVersions[factKind]
	return version, ok
}

// ScannerWorkerFamilyDeclaration is the typed fact-family contract declaration for the
// scanner_worker family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var ScannerWorkerFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "scanner_worker",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "supply_chain_impact",
	ReducerDir:             "go/internal/reducer/supplychain/core",
	ProjectionHook:         "supply_chain_impact",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/supply-chain/impact/findings",
	TruthProfile:           "fixture_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "scanner_worker.analysis", PayloadSchema: "sdk/go/factschema/schema/scanner_worker.analysis.v1.schema.json"},
		{Kind: "scanner_worker.warning", PayloadSchema: "sdk/go/factschema/schema/scanner_worker.warning.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in query read model"},
	},
	Producers: []contract.Producer{
		{ID: "scanner-worker", Repository: "eshu-hq/eshu", Path: "go/internal/collector/scannerworker", AuthorityMode: "core_owned", Kinds: []string{"scanner_worker.analysis", "scanner_worker.warning"}},
	},
})
