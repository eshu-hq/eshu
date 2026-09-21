// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package egress parses hosted collector- and extension-egress policy JSON
// and evaluates fail-closed allow/deny decisions for scheduling.
//
// CollectorPolicy and ExtensionPolicy hold the parsed rules; Decide returns
// an allow or deny action with a reason string. The parent coordinator
// package holds these types on its Config and applies the decisions to
// filter instances it schedules; this package does not filter anything
// itself.
package egress
