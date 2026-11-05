// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reachability plans Go reachability scanner work: one claimable scope
// per Go repository, at the commit Eshu observed.
//
// It decodes the derive_from_go_modules section of a component activation's
// config block and turns one rotation page of repositories into one run and one
// work item per repository. The scope id holds only the canonical repository
// id; the commit and clone URL live in the run's requested_scope_set, which the
// extension host delivers on the claim. This package opens no connection and
// reads no store: the root coordinator passes the page in and owns admission.
//
// StaleEvidence applies the commit-skew rule to the same page: which active
// go-reachability generations measured a commit the repository left more than two
// rotation periods ago. It decides only; the root applies the decision through the
// store.
package reachability
