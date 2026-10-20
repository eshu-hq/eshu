// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package accessanalyzer maps IAM Access Analyzer metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence analyzer, archive-rule, aggregate
// finding-count, and unused-access summary resources plus safe analyzer
// relationships. Finding bodies, archive-rule filters, policy-generation
// results, per-action unused-access details, and mutation APIs stay outside the
// package contract.
package accessanalyzer
