// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tfstate reads Terraform-state backend facts and drift evidence
// for collectors, the reducer, and the AWS runtime-drift consumer. It owns
// two read surfaces over ingestion_scopes, scope_generations, and
// fact_records:
//
//   - Backend facts: TerraformStateBackendFactReader (S3, local, and
//     Terragrunt candidates plus filter/interpolation enrichment),
//     PostgresTerraformBackendQuery (canonical backend rows by locator),
//     TerraformStatePriorSnapshotReader, and
//     TerraformStateGitReadinessChecker.
//   - Drift evidence: PostgresDriftEvidenceLoader.LoadDriftEvidence joins
//     config (terraform_resources), state (terraform_state_resources), and
//     prior-config addresses into per-address drift rows.
//
// The loader takes only db.Queryer and fails closed on a nil DB. Three
// loader methods stay exported because the root AWS runtime-drift consumer
// (still in package postgres until its own move) calls them directly:
// UnresolvedRecorder, BuildModulePrefixMap, and LoadConfigByAddress.
// CoerceJSONString stays exported because the root aws, cloud, and identity
// families call it through the root compat forwarder.
//
// Caller invariants: pass a bounded db.Queryer; the SQL constants in
// backend_queries.go and drift_evidence_sql.go are the
// shipped statements — predicate or column changes need their own issue
// with EXPLAIN proof per eshu-postgres-rigor. flattenStateAttributes must
// stay byte-identical in behavior to the parser's ctyValueToDriftString
// (pinned by drift_evidence_cross_package_encoding_test.go).
// This package must not import the parent postgres package.
package tfstate
