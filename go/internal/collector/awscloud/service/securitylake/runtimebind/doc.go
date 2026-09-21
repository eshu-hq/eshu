// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind self-registers the Amazon Security Lake scanner with the
// AWS collector runtime.
//
// Importing this package for its side effects installs a builder that wires the
// securitylake SDK adapter into the scanner for one claimed boundary. The
// aggregator in awsruntime/bindings blank-imports this package so the scanner is
// available without an explicit reference at the call site.
package runtimebind
