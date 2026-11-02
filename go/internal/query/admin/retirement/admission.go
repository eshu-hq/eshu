// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

// RetirementAdmissionContractVersion versions the #8872
// admission-contract shape for the operator repository retirement trigger
// (POST /api/v0/admin/repository-retirements). Canonical mutations persist
// through explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned Trigger
// satisfies. A breaking change to the trigger shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
const RetirementAdmissionContractVersion = "v0"

// RetirementAdmission is the versioned admission-contract port the API
// handler depends on instead of a direct trigger handle. It aliases the
// narrow trigger interface so existing fakes and the core-owned Trigger
// implementation keep satisfying it unchanged.
type RetirementAdmission = Triggerer

var _ RetirementAdmission = (*Trigger)(nil)
