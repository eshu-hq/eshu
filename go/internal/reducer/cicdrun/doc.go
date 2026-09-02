// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cicdrun materializes the ci_cd_run_correlation reducer domain: it
// joins provider CI/CD run facts (ci.run, ci.artifact,
// ci.environment_observation, ci.deployment_event, ci.trigger_edge, ci.step,
// ci.workflow_image_evidence) with reducer-owned container-image identity
// evidence, and publishes one durable
// [CICDRunCorrelationDecision] per provider run.
//
// [CICDRunCorrelationHandler.Handle]: loads the scope generation's ci.* facts
// (expanding an artifact-only generation into a bounded historical rebuild
// when needed, see ci_cd_run_correlation_patch.go), decodes them through the
// sdk/go/factschema typed seam, joins them against reducer-owned
// container-image identity evidence loaded across scopes, and writes exact,
// derived, ambiguous, unresolved, and rejected decisions so downstream
// domains (supply_chain_impact) can see both truth and suppressed evidence.
// A cross-scope join runs behind the #5709 producer-readiness floor
// ([crossscope.CheckProducerReadinessBeforeLoad]): a correlation that would
// otherwise run before the container_image_identity scope activates defers
// instead of writing a durable "no answer."
//
// This package imports [github.com/eshu-hq/eshu/go/internal/reducer/contract]
// (the dependency-neutral domain/intent/result vocabulary),
// [github.com/eshu-hq/eshu/go/internal/reducer/crossscope] (the shared
// cross-scope readiness floor), [github.com/eshu-hq/eshu/go/internal/reducer/factdecode]
// (quarantine partitioning and telemetry recording),
// [github.com/eshu-hq/eshu/go/internal/reducer/factload] (the scoped fact
// loader), [github.com/eshu-hq/eshu/go/internal/reducer/factwrite] (batched
// fact-row inserts), [github.com/eshu-hq/eshu/go/internal/reducer/payloadcore]
// (deref/trim/convert helpers), and
// [github.com/eshu-hq/eshu/go/internal/reducer/schemadecode] (the typed-payload
// decode seam), plus [github.com/eshu-hq/eshu/go/internal/facts] and the
// generated sdk/go/factschema/cicdrun/v1 package. It must never import the
// parent reducer package or a sibling domain-family subpackage — see
// AGENTS.md.
package cicdrun
