// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capability

// catalogUnavailableMessage is the fixed body a failed catalog load answers
// through tracing.WriteServerFailure (#7674): 500 with the load error on the
// request span, or 499 with only the client-cancel event when the caller
// canceled. The load error never reaches the response body.
const catalogUnavailableMessage = "capability catalog unavailable"
