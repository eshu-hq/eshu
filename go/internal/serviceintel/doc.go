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
package serviceintel
