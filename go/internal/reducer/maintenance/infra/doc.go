// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package infra re-derives the infra read model rows (#6793) of any
// repository whose rows differ from content_entities on two consecutive
// cycles.
//
// [Runner] walks repositories in bounded, keyset-ordered cycles from a
// persisted cursor, so replicas take disjoint pages and a restarted
// process resumes the walk. A repository that differs once is a suspect,
// re-checked first next cycle and repaired only if it still differs, so
// a write caught between its content commit and its derive is neither
// repaired nor reported as drift. Writes from a binary that does not
// derive are fenced: re-derived immediately while readers stay on the
// graph until they are.
package infra
