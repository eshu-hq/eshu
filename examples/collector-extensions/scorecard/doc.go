// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scorecard converts OpenSSF Scorecard-style JSON into Eshu collector
// SDK result records.
//
// The package is a reference out-of-tree collector extension. It imports only
// the public collector SDK, emits namespaced evidence facts, and leaves graph
// truth, reducer admission, hosted scheduling, and component trust decisions to
// core Eshu.
package scorecard
