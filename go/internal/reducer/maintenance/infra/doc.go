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
//
// [BackfillRunner] starts the backfill that records the read model's marker
// on a fresh install, a wiped database, or a marker bump (#11196). Every
// reducer replica runs it; the per-repository derive lock and the idempotent
// marker insert make concurrent passes safe.
package infra
