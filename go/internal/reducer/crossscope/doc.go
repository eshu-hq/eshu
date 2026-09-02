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
// supply_chain_impact both call CheckProducerReadinessBeforeLoad,
// UnreadyProducers, and LogProducerNotReadyDefer, and both are declared as
// consumers in the dependency catalog this package also owns. That is what
// makes it a genuine shared tier rather than a one-family helper: it moved
// here, rather than into either family's subpackage, precisely because
// neither family owns it alone.
//
// This package imports internal/reducer/contract (the dependency-neutral
// domain/intent vocabulary) and internal/reducer/factload (for the fact-load
// error classifier the readiness probe reuses), and nothing else outside the
// standard library and internal/eshu-hq/go/pkg/log. It must never import the
// parent reducer package or a domain-family subpackage — see AGENTS.md.
package crossscope
