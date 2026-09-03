// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package platformfam owns the reducer's platform vocabulary and the
// deployment_mapping reduction that turns a platform-binding intent into a
// canonical fact.
//
// Two pieces live here. The Terraform runtime-family registry answers "which
// runtime does this infrastructure describe": RuntimeFamilies enumerates the
// eight registered families, LookupRuntimeFamily resolves one by kind, and the
// Infer* functions read a family out of Terraform content, repo identifiers, or
// a resource-type/module-source pair. The deployment_mapping handler,
// PlatformMaterializationHandler, validates one intent into a bounded
// PlatformMaterializationWrite, persists it through a
// PlatformMaterializationWriter (PostgresPlatformMaterializationWriter is the
// production one), optionally resolves cross-repo dependency edges, replays
// workload materialization when those edges landed, and publishes the
// deployment_mapping graph-readiness phase.
//
// PROVISIONS_PLATFORM edges are not written here. The dedicated
// platform_infra_materialization domain owns that verb and still lives in the
// reducer root, because it depends on the root's InfrastructurePlatform
// extractor and materializer.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches [reducercontract],
// [factwrite], [gpphase], [payloadcore], internal/facts and pkg/log, and never
// the parent internal/reducer package. The reducer root keeps compatibility
// aliases in platform_compat.go so its own callers, cmd/reducer and
// internal/storage/postgres compile unchanged; that direction is root importing
// this family, never the reverse. See AGENTS.md in this directory before adding
// an import.
//
// CrossRepoRelationshipResolver exists for that reason. The handler needs the
// root's CrossRepoRelationshipHandler, so it names the behaviour it depends on
// -- one Resolve call per scope generation returning a canonical edge count --
// instead of the concrete type. A caller assigning a nil concrete pointer into
// that interface field would defeat the handler's nil guard, so the root's
// default domain catalog assigns it only when the cross-repo dependencies were
// actually wired.
//
// # Observability
//
// This package emits no metric of its own. Reducer executions that run these
// handlers stay covered by eshu_dp_reducer_executions_total and
// eshu_dp_reducer_run_duration_seconds, and the canonical fact the writer
// publishes flows through Result.CanonicalWrites into
// eshu_dp_canonical_writes_total. Handle logs "deployment mapping
// materialization completed" with the per-stage wall times, and returns them
// again in Result.SubDurations (platform_write, cross_repo_resolve,
// workload_replay, phase_publish, total) alongside the input_ready and
// written_rows values in Result.SubSignals, which the reducer service layer
// emits as sub_duration_<key>_seconds and sub_signal_<key>.
package platformfam
