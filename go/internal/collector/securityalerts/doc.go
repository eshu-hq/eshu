// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package securityalerts normalizes repository-scoped provider security alert
// evidence into durable source facts.
//
// This package owns GitHub Dependabot alert normalization and the bounded
// request client used by the hosted claim-driven runtime. Emitted facts
// preserve provider alert identifiers, state, dependency coordinates, advisory
// identifiers, version ranges, severity, CVSS, EPSS, CWE, timestamps, and
// source URLs with reported confidence. They are not canonical Eshu impact
// truth; reducers reconcile them with Eshu-owned dependency and vulnerability
// evidence.
package securityalerts
