// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

// languageQueryFailedMessage is the fixed body a failed language-query read
// answers through tracing.WriteServerFailure (#7674), after
// querycontract.WriteGraphReadError has taken the fence and graph-availability
// verdicts: 500 with the read error on the route span, or 499 with only the
// client-cancel event when the caller canceled.
const languageQueryFailedMessage = "language query failed"
