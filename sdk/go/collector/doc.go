// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector defines the public Eshu collector extension SDK contracts.
//
// The SDK is intentionally independent from Eshu internal Go packages. It
// describes the collector-sdk wire records an out-of-tree collector can
// return to a core-owned extension host, plus validators that fail closed before
// the host commits facts. The v1alpha1 single-shot shape is unchanged;
// collector-sdk/v1alpha3 adds the optional Result.Page continuation so a
// result that would not fit one bounded stdout write splits into resumed
// calls, with byte-accounting helpers (PageBoundForStdout, FactWireBytes,
// SplitFacts) that size every page against the host bound.
package collector
