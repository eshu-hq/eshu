// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TerminalFailureAdmissionContractVersion versions the #8872
// admission-contract shape for the operator terminal-failure
// acknowledgement mutation (POST
// /api/v0/admin/collector-failures/acknowledge). Canonical mutations
// persist through explicit core-owned admission contracts: eshu-api keeps
// the HTTP shape and calls this versioned port, which the core-owned
// Postgres store satisfies. A breaking change to the admission shape must
// bump this version in the same diff.
const TerminalFailureAdmissionContractVersion = "v0"

// TerminalFailureAdmission is the versioned admission-contract port the
// API handler depends on instead of a direct table handle. It aliases the
// narrow family store so existing fakes and the core-owned Postgres
// implementation keep satisfying it unchanged. The replay idempotency
// ledger the acknowledgement shares with the replay, reopen, and
// recover-generations routes is not versioned here; it stays a documented
// gap until its own contract lands.
type TerminalFailureAdmission = Acknowledger

var _ TerminalFailureAdmission = (*pgstorage.TerminalFailureAckStore)(nil)
