// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package poison bounds-recovers the dead-letter/poison class (#4740):
// fact_work_items rows that are terminally 'dead_letter' with no newer
// scope generation, the class the generation-liveness sweep cannot reach
// because such a scope has no ACTIVE generation to begin with.
//
// [Runner] only re-drives a dead-letter row when [Config].AutoRetryEnabled
// is true. The stuck-gauge reporting the poison class size is wired
// independently in cmd/reducer and remains active regardless of this flag.
package poison
