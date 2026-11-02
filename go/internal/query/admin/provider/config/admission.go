// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

// ProviderConfigMutationAdmission is the #8872 admission boundary for
// admin provider-config mutations: POST create, update, revert, enable,
// and disable on
// /api/v0/auth/admin/provider-configs. Write-only secrets stay sealed —
// no response, log line, or audit event ever carries plaintext or sealed
// ciphertext — and every attempt is governance-audited.
//
// It aliases MutationStore so the boundary is identity-verified by the
// frozen contract test rather than a parallel hand-maintained shape; it
// will back the eshu-api extraction handoff for this family.
type ProviderConfigMutationAdmission = MutationStore

// ProviderConfigMutationAdmissionContractVersion pins the versioned shape
// of ProviderConfigMutationAdmission. Any breaking change to the method
// set must bump this alongside the frozen-list update in
// mutation_admission_contract_test.go so dest consumers can pin the
// shape.
const ProviderConfigMutationAdmissionContractVersion = "v0"

// ProviderConfigReadAdmission is the #8872 admission boundary for the
// reads backing the mutation enable-time login-readiness guard (issue
// #5604): re-reading stored non-secret configuration to check for fields
// ResolveSealedProviderConfig needs at login. It aliases ReadStore so
// the boundary is identity-verified by the frozen contract test; it will
// back the eshu-api extraction handoff alongside the mutation contract.
type ProviderConfigReadAdmission = ReadStore

// ProviderConfigReadAdmissionContractVersion pins the versioned shape of
// ProviderConfigReadAdmission. Any breaking change to the method set
// must bump this alongside the frozen-list update in
// read_admission_contract_test.go so dest consumers can pin the shape.
const ProviderConfigReadAdmissionContractVersion = "v0"
