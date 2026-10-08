// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

// Fixed response bodies for a content read or search that failed after the
// selector resolved (#7626). A store error quotes SQL and backend detail, so
// the client sees only one of these; the error itself goes to the request
// span through tracing.WriteServerFailure.
const (
	contentFileReadFailedMessage     = "content file read failed"
	contentEntityReadFailedMessage   = "content entity read failed"
	contentFileSearchFailedMessage   = "content file search failed"
	contentEntitySearchFailedMessage = "content entity search failed"
)
