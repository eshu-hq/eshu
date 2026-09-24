// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package maintenancestore persists the operator-triggered ingester
// scan/reindex request lifecycle: a request moves from idle to pending,
// pending to running on claim, and running to completed or failed on
// completion, tracked per ingester in the runtime_ingester_control table.
//
// StatusRequestStore implements runtime.StatusRequestStore over an injected
// db.ExecQueryer. It carries no queue leasing, no fencing token, and no
// dependency on the rest of the postgres root: every statement addresses one
// ingester row keyed by its primary key. This package must not import the
// parent postgres package.
package maintenancestore
