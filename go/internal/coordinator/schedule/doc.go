// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package schedule holds the coordinator's shared scheduling substrate:
// target-class constants and ranking, derived-target skip evidence and
// budget accounting, derived-target rotation offsets and plan keys, read
// limits, and small string-set and version helpers.
//
// The parent coordinator package and its vulnerability and
// registry/package families call these functions to keep target ranking,
// plan-key rendering, and rotation paging identical across scheduler
// implementations. This package imports nothing from the coordinator root.
package schedule
