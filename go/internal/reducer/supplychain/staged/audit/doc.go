// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package audit runs the staged stale-winners audit as a sweep of identifier
// ranges over the keyed winners index (eshu-hq/eshu#11131, replacing the
// whole-corpus tick of #10451). Each AuditWinners call runs one range: one
// standby statement recomputes the range's winners and diffs them against the
// stored winners in the same key interval, then one writer transaction
// verifies the differing identifiers, queues the stale ones, and advances
// the persisted cursor by compare-and-set. It implements the reducer core's
// auditor interface; the maintainer runs ranges back to back while a pass
// reports More, within its tick budget, renewing the audit lease per range.
package audit
