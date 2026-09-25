// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package organizations converts AWS Organizations metadata into AWS cloud
// collector observations.
//
// The package emits reported-confidence facts for organization roots, OUs,
// accounts, policy summaries, policy target bindings, and delegated
// administrators. Account email and account name values are redacted through
// the shared AWS redaction helper before they can enter fact payloads.
//
// The scanner is metadata-only: it does not call Organizations mutation APIs,
// read or persist policy document bodies, or infer account ownership,
// deployment, workload, or graph truth from names or policy bindings.
package organizations
