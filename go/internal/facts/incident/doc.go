// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package incident owns incident-management source fact kinds: provider
// reported operational incidents, their lifecycle events and related change
// records (context.go), plus incident-routing evidence observed from applied
// Terraform state and the live PagerDuty API (routing.go).
//
// The kinds declared here are source evidence only. Reducers own incident
// context graph and read-model truth; this package decides no admission and
// projects nothing. Family declarations use contract.Declare, the identity
// function the static registry generator reads; see the fact-kind registry
// for the generated kind rows.
//
// The package depends only on the standard library and facts/contract. It
// must not import the parent facts package: the schema registry and family
// callers reach in through this package, never the reverse.
package incident
