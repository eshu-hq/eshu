// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admissionstore persists shared reducer admission decisions and
// their evidence handles for the admission_decisions and
// admission_decision_evidence tables.
//
// AdmissionDecisionStore.UpsertDecision records the closed
// AdmissionDecisionState vocabulary (admitted, rejected, ambiguous, stale,
// missing_evidence, permission_hidden, unsupported, unsafe) a correlation
// domain reached for one anchor/candidate pair inside one scope and
// generation, including the redaction-safe AdmissionDecisionSourceHandle
// list, the AdmissionDecisionCanonicalWrite eligibility/write posture, and
// an AdmissionDecisionNextAction for operator follow-up.
// AdmissionDecisionStore.InsertEvidence records detailed, source-attributed
// evidence rows per decision. ListDecisions and ListEvidence require a
// bounded domain/scope/generation filter (or decision id) and clamp their
// page size, matching every other #6693 read store: unbounded reads are
// answered against generation-scale data.
//
// This package owns no correlation domain logic: it does not decide
// admission states, only persists and reads back what a reducer domain
// (go/internal/reducer/admissiondecision and its callers) already decided.
//
// This package must not import the parent postgres package.
package admissionstore
