// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// SpanAttrRecoveryDeltaActiveScopes records, on the caller's span, how many
// scopes one refinalize re-enqueued through a delta generation (#7797). A
// non-zero value means the rebuilt graph is incomplete for those scopes until
// a full generation activates.
const SpanAttrRecoveryDeltaActiveScopes = "eshu.recovery.delta_active_scopes"
