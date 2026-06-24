// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package serviceintel composes existing Eshu answer evidence into a
// deterministic, operator-ready service intelligence report.
//
// The package is intentionally a pure composer. Callers gather evidence from
// existing answer routes — the service story dossier, supply-chain impact
// inventory, incident context, and evidence citation packets — and hand it to
// Compose as per-section SectionInput values. Compose arranges those inputs
// into a fixed, ordered Report. It never queries a store, runs an LLM
// interpretation path, or reclassifies truth: each section's truth is the
// source route's TruthEnvelope, classified by the canonical AnswerPacket
// builder in package query, and the report-level truth is anchored on the
// identity section.
//
// The report's product promise is honesty under composition. A section without
// resolved evidence does not vanish or become confident prose: it stays visible
// as partial or unsupported, drops its summary, and carries an explicit
// limitation plus a bounded next call that names a real tool, route, or query
// playbook. Compose is deterministic, so the same evidence always yields a
// byte-identical report suitable for diffing, caching, and dogfood scoring.
//
// Compose also derives guided SuggestedInvestigation values from the report's
// own signals: unresolved evidence handles, stale or building freshness with a
// proven cause, ambiguous service targets, unavailable evidence lanes, and
// caller-flagged high-impact relationships. Each suggestion ties a concrete
// signal to a bounded next call and a grounded expected truth class; the list
// is de-duplicated, bounded, and empty when no section carries a supporting
// basis. Suggestions never choose a winner for an ambiguous target; they
// recommend resolving it.
//
// FromServiceStory adapts a get_service_story dossier response into a ReportInput
// (the subject plus the identity, code_to_runtime, and deployment_config
// sections), so a caller can build a report from real route evidence without
// hand-assembling SectionInput values. It is a faithful translation: it reads
// only confirmed dossier fields and section cardinalities, carries the source
// truth envelope verbatim, and never invents evidence. Callers append
// supply-chain and incident sections from their own routes before calling
// Compose.
package serviceintel
