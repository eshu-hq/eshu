// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packetdogfood scores the investigation evidence packet dogfood
// benchmark: whether Eshu's portable v2 evidence packets produce a faster and
// more trustworthy first useful answer than raw repository search or an existing
// Eshu tool drilldown.
//
// The package is pure and deterministic. ParseBenchmark decodes and structurally
// validates a captured benchmark artifact, and Score evaluates it across five
// scoring dimensions from issue #3143 — family coverage, answer correctness,
// answer time, token budget, and missing-evidence clarity — returning a Verdict
// the CLI renders and uses as a pass/fail gate. The benchmark covers tasks for
// supply-chain impact, deployable drift, and service context, each measuring the
// raw-files, eshu-tools, and evidence-packet approaches.
//
// Scoring rules: the evidence-packet approach must cover the required families
// and, on every task, find the answer at least as fast as the best baseline,
// stay within the best baseline's token budget, and name missing evidence —
// including a gap on at least one task that every baseline missed, which is the
// trustworthiness differentiator the benchmark exists to prove.
//
// The package performs no graph, content, provider, or network reads; the
// benchmark artifact is captured separately (one reproducible fixture run plus,
// when supplied, one real-repository run) and only scored here.
package packetdogfood
