// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tfconfigstate reconciles Terraform config facts (parsed HCL)
// against Terraform state facts to detect and durably publish drift for one
// state-snapshot scope at a time.
//
// [TerraformConfigStateDriftHandler] is the reducer intent handler for the
// contract package's DomainConfigStateDrift. It resolves the config repo that
// owns a state snapshot's backend via a tfstatebackend.Resolver, loads the
// joined per-address rows through [DriftEvidenceLoader], and hands the
// candidates to the correlation engine (BuildCandidates in
// internal/correlation/drift/tfconfigstate) to classify each address into one
// of five drift kinds and record a deterministic explain trace. The handler
// is deliberately tolerant of a nil Resolver or EvidenceLoader on
// [TerraformConfigStateDriftHandler]: both make it treat every intent as
// having no observable input and return success without drift, rather than
// fail — this domain has no fatal-by-construction input.
//
// [TerraformConfigStateDriftFindingWriter] persists what the handler found.
// A call carries exactly one of an admitted candidate set (outcome "exact" or
// "derived"), an ambiguous-backend-owner rejection (outcome "ambiguous"), or
// an unresolved-owner rejection (outcome "unresolved", issue #5594) — see
// [TerraformConfigStateDriftWrite] for which. The writer must be idempotent
// by finding identity so reducer retries never duplicate a row, and
// [PostgresTerraformConfigStateDriftWriter] additionally retires every prior
// generation's finding the current write superseded
// (terraform_config_state_drift_writer_retire.go), so a resolved backend or a
// backend that stops drifting does not leave stale findings behind.
//
// An ambiguous-owner write failure is swallowed (logged, not returned) because
// a resolvable ambiguity gets a fresh chance on the next state-snapshot
// generation regardless of whether this write landed. An unresolved-owner
// write failure is NOT swallowed — see the comment on writeUnresolvedOwner in
// terraform_config_state_drift_unresolved_owner.go for why a permanently
// unresolved backend has no such future generation to retry against.
package tfconfigstate
