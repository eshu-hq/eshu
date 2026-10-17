// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidence publishes the evidence-citation handle vocabulary that
// sibling packages compose into derived AnswerPacket views.
//
// EvidenceCitationHandle is a type alias for the canonical
// querycontract/evidence handle wire shape, not a redefinition: every field
// stays exported and unchanged, so handles built through this package and
// handles built through querycontract/evidence are the same value. The
// evidence-citation endpoint itself (POST /api/v0/evidence/citations),
// the citation synthesis helpers, and the EvidenceHandler stay in the root
// query package until the full evidence leaf move (#6642); this package
// must never import the root query package.
package evidence
