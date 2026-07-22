// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the eshu-reducer binary, the long-running runtime that
// drains the reducer fact-work queue, executes domain handlers, materializes
// cross-domain truth, and writes shared edges into the configured graph
// backend.
// It also wires the shared admission-decision postgres adapter for mapped
// reducer domains that publish explainable admission outcomes beside their
// existing canonical graph or fact writers.
//
// When invoked with --version or -v, it prints the embedded application
// version and exits before runtime setup. Otherwise the binary boots OTEL
// telemetry, opens Postgres and the graph backend,
// wires the reducer service with shared-projection, code-call,
// repo-dependency, graph-projection-phase repair, generation-retention cleanup,
// graph orphan sweep, value-flow stale-evidence cleanup, and opt-in local
// search-vector build runners and the canonical edge writer used by direct
// CODEOWNERS ownership materialization (plus the
// Terraform config-vs-state drift handler wired with the
// PostgresTerraformBackendQuery and PostgresDriftEvidenceLoader adapters;
// the loader's PriorConfigDepth field is set from ESHU_DRIFT_PRIOR_CONFIG_DEPTH
// via parsePriorConfigDepth (default 10, 0 means use default, invalid input
// falls back to 0 with a WARN log); the loader receives the runtime tracer
// so its four-input join surfaces as one SpanReducerDriftEvidenceLoad span
// over InstrumentedDB children, plus the runtime *telemetry.Instruments
// handle so the module-aware join in issue #169 can increment
// eshu_dp_drift_unresolved_module_calls_total when a module call's source
// resolves to a registry, git URL, archive, or cross-repo path; the AWS runtime
// drift durable fact writer is wired for issue #39, the SBOM/attestation
// attachment writer publishes digest-subject attachment facts, and the
// supply-chain impact writer publishes reducer-owned vulnerability impact
// facts without graph writes, the EC2 internet-exposure writer sets
// reducer-owned exposed/not_exposed/unknown properties on existing EC2
// CloudResource nodes, and the S3 internet-exposure writer sets the same
// bounded property family on existing S3 CloudResource nodes, while the Azure
// relationship edge writer projects managed ARM relationships only after Azure
// CloudResource endpoint readiness, and the value-flow fixpoint projector writes
// post-summary TAINT_FLOWS_TO evidence, including graph-backed cloud sink
// targets, under its own evidence source with durable component-result reuse),
// and hosts it
// through app.NewHostedWithStatusServer so it exposes the shared `/healthz`,
// `/readyz`, `/metrics`, and `/admin/status` admin surface. NornicDB reducer
// workers default to NumCPU, while Neo4j remains capped lower by default; the
// local-authoritative NornicDB profile also wires a reducer graph-drain gate for
// code-call projection so graph write lanes do not compete unnecessarily.
// NornicDB SQL relationship writes use one-statement autocommit because its
// managed-transaction path can acknowledge UNWIND/MATCH/MERGE without
// persisting the relationship; Neo4j retains grouped transaction dispatch.
// SIGINT and SIGTERM trigger clean shutdown through the hosted runtime drain.
//
// When ESHU_PPROF_ADDR is set, the binary also exposes an opt-in
// net/http/pprof endpoint via runtime.NewPprofServer, bound to 127.0.0.1
// for port-only inputs so the default does not reach beyond the local host.
package main
