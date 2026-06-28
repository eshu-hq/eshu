// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package schemareplay is the schema-version compatibility replay for the
// deterministic replay framework (design doc 4102 §11, R-18 / #4127).
//
// It replays a small FROZEN corpus of old-schema cassettes — facts recorded at
// historical fact_schema_version values — against the CURRENT admission code and
// asserts every fact reaches a defined outcome: it is either admitted (the
// version is still supported, or core owns no versioned schema for the kind) or
// CLEANLY REFUSED with an explicit error. A fact is never silently projected
// under the wrong interpretation.
//
// ReplayAdmission drives each recorded fact through the production
// facts.ValidateSchemaVersion — the same per-fact AdmissionHook the projector
// wires (projector/schema_version_admission.go delegates to it) — so the gate
// asserts the real admission decision, not a parallel re-implementation. It
// needs no graph backend and no Postgres and runs in the default `go test` pass.
//
// The corpus is tied to the central fact-schema-version registry (#3152) by a
// pin guard: if a contributor bumps a fact's supported schema_version, the guard
// fails until they add a frozen replay case proving the older version still
// admits (a migration path) or an explicit asserted refusal — so cross-version
// drift cannot land silently.
package schemareplay
