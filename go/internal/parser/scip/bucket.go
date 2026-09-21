// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scip

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// appendBucket appends one row to a payload bucket, allocating the slice on
// first use. It mirrors shared.AppendBucket for the SCIP payload buckets.
// This package keeps its own thin wrapper (matching the per-package copies in
// internal/parser/sql, internal/parser/java, and internal/parser/python)
// rather than importing the parent internal/parser package, which would risk
// an import cycle and defeat the point of the SCIP package split.
func appendBucket(payload map[string]any, key string, item map[string]any) {
	shared.AppendBucket(payload, key, item)
}
