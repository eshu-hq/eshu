// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtime groups AWS projector intent builders driven by observed
// cloud runtime state. It is a documentation-only namespace and owns no
// runtime behavior: leaf packages select trigger facts and return
// reducer-intent values, root projector assembly owns lookup construction,
// ordered fan-out, queue writes, retries, and telemetry, and reducer
// packages own graph materialization.
package runtime
