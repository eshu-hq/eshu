// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kubernetes decides whether a Kubernetes Service selects a workload.
//
// One question is asked from several places -- the content relationship
// builder, the deployment trace, the impact handlers -- and every caller has to
// answer it the same way or the graph disagrees with itself. That answer lives
// here once.
//
// A Service selects a workload for one of exactly two reasons, and which one
// matters on the wire:
//
//   - [SelectReasonSelectorMatch], when the Service's selector is a subset of
//     the workload's pod-template labels. This is a proof.
//   - [SelectReasonNameNamespace], when the Service's selector state is
//     unknown, so name and namespace equality stands in for it. This is an
//     inference, and it is reachable only from a pre-upgrade content row whose
//     SelectorPresent is false.
//
// A row that predates selector capture cannot be distinguished from a Service
// whose selector is genuinely empty by the selector string alone, which is why
// [SelectMatchInput] carries SelectorPresent beside Selector rather than
// relying on the empty string. SelectorPresent false means "not captured", and
// only that case falls back to name and namespace.
// When a match would mix a selector-bearing side with a pre-upgrade side, the
// edge is dropped rather than guessed, and [LogSelectMixedVintageDrop] records
// it at debug so an operator can see why an expected edge is missing.
//
// [Namespace] exists so that every path deriving a namespace derives it
// identically. Namespace equality gates matching, so two copies of the trim
// could drift apart without anything failing.
//
// The package depends on its parent for three shared types and nothing else,
// and the parent does not depend on it. Callers name these identifiers
// directly: the compatibility wrappers that once carried them on package query
// were removed with the move (#6642 retains no aliases).
package kubernetes
