// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// RecoveryAdmissionContractVersion versions the #8872
// admission-contract shape for the core recovery operations the admin
// endpoints admit through RecoveryService (POST /api/v0/admin/refinalize
// and POST /api/v0/admin/recover-generations). Canonical mutations persist
// through explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned recovery
// Handler satisfies. A breaking change to the recovery shape fails the
// frozen method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
const RecoveryAdmissionContractVersion = "v0"

// RecoveryAdmission is the versioned admission-contract port the API
// handler depends on instead of a direct recovery handle. It aliases the
// narrow recovery service so existing fakes and the core-owned Handler
// implementation keep satisfying it unchanged.
type RecoveryAdmission = RecoveryService

var _ RecoveryAdmission = (*recovery.Handler)(nil)
