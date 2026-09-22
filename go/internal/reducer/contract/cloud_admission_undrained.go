// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "errors"

// ErrCloudAdmissionUndrained is the sentinel the #6887 cloud-resource
// live-check wraps when some scope's active generation still has a
// nonterminal cloud_inventory_admission work item. reducer_cloud_resource_identity
// is reducer output written by that separate, unfenced work item, so until it
// drains the scope has no admission rows for its active generation and every
// uid it still holds would read dead to another scope's retract. The check
// refuses to prove death instead. The storage layer wraps this sentinel with
// the scopes it found; the reducer handler classifies it as a non-counting
// readiness miss (CloudAdmissionNotReadyFailureClass) so the durable queue
// retries the intent without eroding its attempt budget. It lives in this
// package because storage/postgres raises it and internal/reducer inspects
// it, and neither may import the other.
var ErrCloudAdmissionUndrained = errors.New("cloud inventory admission not drained for an active generation")
