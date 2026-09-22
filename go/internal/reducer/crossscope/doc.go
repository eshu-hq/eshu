// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package crossscope holds the cross-scope producer-readiness floor: the
// correctness gate (#5709) that defers a cross-scope consumer domain until the
// producer domains it depends on have activated for the relevant scope, plus
// the dependency catalog that declares which consumer depends on which
// producers.
//
// Every symbol here qualifies by the same criterion the package restructure
// uses everywhere: it is meaningful with any one family deleted, because it
// is read by MORE than one family. ci_cd_run_correlation and
// supply_chain_impact both call CheckProducerReadinessBeforeLoadWithLedger
// and ApplyProducerReadinessPostLoad (the ledger-anchored #6814 pair built on
// UnreadyProducers and DecideWait), and both are declared as consumers in the
// dependency catalog this package also owns. That is what
// makes it a genuine shared tier rather than a one-family helper: it moved
// here, rather than into either family's subpackage, precisely because
// neither family owns it alone.
//
// It also owns the commit-first readiness wait (#6785) that the CAN_PERFORM
// and USES handlers share: [DecideWait] is a pure decision over a
// [ReadinessWait] ledger row keyed by (scope_id, domain), whose first-defer
// anchor survives supersession of the per-generation queue row. Handlers
// commit ready edges first, then wait, and write the ledger through
// [ReadinessWaitLedger] only after the graph commit.
//
// It also owns the #6923 value-flow-inputs readiness class
// ([ValueFlowInputsNotReadyFailureClass], [ClassifyValueFlowInputsError],
// [WrapValueFlowInputsUndrained]): a DIFFERENT gate from the producer-scope
// floor above, declared here only so the go/ast enrollment guard
// (TestEveryReadinessFailureClassIsEnrolled in internal/storage/postgres,
// which walks internal/reducer and its immediate subdirectories only) can
// see it. code/value/refresh.Handler runs its own SQL fence
// (storage/postgres.ValueFlowInputsLivenessStore) directly; it does not call
// CheckProducerReadinessBeforeLoad or go through the dependency catalog.
//
// This package imports internal/reducer/contract (the dependency-neutral
// domain/intent vocabulary) and internal/reducer/factload (for the fact-load
// error classifier the readiness probe reuses), and nothing else outside the
// standard library and github.com/eshu-hq/eshu/go/pkg/log. It must never import the
// parent reducer package or a domain-family subpackage — see AGENTS.md.
package crossscope
