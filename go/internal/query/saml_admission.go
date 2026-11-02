// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// SAMLLoginAdmission is the #8872 admission boundary for SAML login:
// GET metadata, GET login, and POST ACS on
// /api/v0/auth/saml/providers/{provider_id}/*. It aliases SAMLStore so
// the boundary is identity-verified by the frozen contract test rather
// than a parallel hand-maintained shape; it will back the eshu-api
// extraction handoff for this family.
//
// Sessions, the assertion verifier, and the sign-in policy reader stay
// wired alongside the store and outside this contract, mirroring how the
// OIDC and GitHub login contracts leave session issuance to the
// BrowserSessions family's own seam.
type SAMLLoginAdmission = SAMLStore

// SAMLLoginAdmissionContractVersion pins the versioned shape of
// SAMLLoginAdmission. Any breaking change to the method set must bump
// this alongside the frozen-list update in
// saml_login_admission_contract_test.go so dest consumers can pin the
// shape.
const SAMLLoginAdmissionContractVersion = "v0"
