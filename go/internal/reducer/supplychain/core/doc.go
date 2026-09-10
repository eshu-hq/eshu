// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package core holds the supplychain family: the supply-chain impact
// correlation builders, the vulnerability suppression evaluation, the
// Postgres writers, and the Go-vulnerability reachability classifier
// (issue #6061).
//
// The family reads vulnerability-intelligence facts (CVE, affected package /
// product, OS package, EPSS, KEV, suppression), package-registry and
// manifest/lockfile dependency evidence, SBOM and container-image identity
// evidence, repository facts, CI run correlation output, and provider
// security alerts, and writes reducer-derived supply-chain impact findings
// plus suppression decisions. Its handler is driven by the reducer runtime
// through the default domain catalog like every other family; see the parent
// package's registry for construction.
//
// Dependency rule: this package imports the shared tier (contract, factload,
// factdecode, factwrite, schemadecode, crossscope, payloadcore,
// supplychainmodel) and the already-extracted cicdrun, containerimage,
// servicecatalog, correlation, source, and securityalert subpackages one-way
// (only their exported consts, fact kinds, and alert/consumption types), plus
// facts, packageidentity, telemetry, truth, environment, and the SDK
// factschema. It never imports the parent reducer package, and the parent
// references it only through supplychaincore-qualified names — external
// callers keep their reducer.X spelling through the supply_chain_impact
// stanza in the parent's compat_correlation.go.
//
// The exported surface is the join contract other families program against:
// the impact builders and finding type (BuildSupplyChainImpactFindings,
// SupplyChainImpactFinding), the suppression evaluation
// (BuildVulnerabilitySuppressions, EvaluateSupplyChainSuppression,
// SupplyChainSuppressionDecision), the handler and writers
// (SupplyChainImpactHandler, PostgresSupplyChainImpactWriter,
// SupplyChainImpactWinnersMaintainer), and the Go reachability classifier
// (ClassifyGoVulnerabilityReachability). Test seams for the reducer root's
// own wiring test live in the parent's compat stanza, following the
// containerimage precedent; family-local test doubles live in
// supply_chain_impact_cross_scope_test_doubles_test.go.
package core //nolint:dirgate // supplychain family for #6061: 71 non-test files vs the 40-file cap; the tree doc names supplychain/core as the single destination and the Suppression-on-SupplyChainImpactFinding field makes finding+core+suppression indivisible, so splitting the directory mid-move would re-create the root<->package cycle the unit move exists to end.
