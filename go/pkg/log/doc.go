// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package log provides canonical slog attribute constructors and key constants
// for the Eshu data plane structured logging contract.
//
// Every exported function returns a slog.Attr with a stable key name.  Keys that
// overlap with the frozen telemetry contract (go/internal/telemetry) reference
// the canonical constant so a rename propagates automatically.
//
// Callers must use these constructors instead of raw slog.String("key", value)
// so an operator can grep logs for known key names with confidence that the
// same key carries the same semantics across binaries and packages.  The package
// does not allocate or create loggers; it only manufactures attributes.
package log
