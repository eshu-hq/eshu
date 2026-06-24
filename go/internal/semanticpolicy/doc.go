// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticpolicy evaluates hosted semantic extraction policy.
//
// The package is a pure contract layer: it parses source allowlists and
// semantic provider egress rules, validates scope and source-class policy, and
// returns reason-coded decisions without loading provider credentials, opening
// storage, or constructing prompts. Callers must pass fresh provider status and
// ACL state for the specific source; missing policy, unsupported source
// classes, stale ACLs, missing or denied egress, and unallowlisted scopes fail
// closed before provider work can be queued.
//
// Evaluate is the full source-level decision used at planning time. EvaluateEgress
// is the focused claim-path egress re-check used by the semantic-provider
// execution worker immediately before any provider dispatch: it re-confirms only
// the provider-profile and source-class egress posture (which can change between
// planning and dispatch) and fails closed on a missing policy, missing allow
// rule, or explicit deny.
package semanticpolicy
