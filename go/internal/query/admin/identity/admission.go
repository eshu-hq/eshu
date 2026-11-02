// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

// AdminIdentityAdmissionContractVersion versions the #8872
// admission-contract shape for tenant-scoped admin identity mutations
// (invitation revoke, role grant/revoke, IdP group mapping
// create/delete). Canonical mutations persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned Postgres store
// satisfies. A breaking change to the mutation shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list
// is the enforcement) so dest consumers can pin the shape.
const AdminIdentityAdmissionContractVersion = "v0"

// AdminIdentityAdmission is the versioned admission-contract port the API
// handler depends on instead of a direct store handle. It aliases the
// narrow mutation store so existing fakes and the core-owned Postgres
// implementation keep satisfying it unchanged.
type AdminIdentityAdmission = MutationStore
