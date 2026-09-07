// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
)

// This file is part of the #6060 lane-A L3 shim: same-name forwarders and
// aliases for the root symbols whose canonical definitions moved to
// internal/query/codeshaping, so the code-family files that have not moved
// yet keep compiling with zero edits to their files. The deletion protocol
// in family_code_shim.go's header applies to every entry here: delete each
// entry with its named big-bang handler move, when the last family-file
// user moves and qualifies its call sites. The file itself is deleted when
// it empties.
//
// The file is deliberately NOT named code_shaping_shim.go: the lane-A family
// is the root non-test ^(code|complexity|dead_code) set, and a code_-prefixed
// staying file would match a later phase's family glob and be swept into a
// move it must survive (family_code_shim.go precedent).

// deadCodeCandidateSchedule aliases the leaf-owned candidate-scan schedule
// so the staying dead-code scan, investigation, and cross-repo readers keep
// their declarations unchanged. Delete with the #6060 big-bang handler
// move of code_dead_code_scan.go, code_dead_code_investigation.go, and
// code_dead_code_cross_repo.go.
type deadCodeCandidateSchedule = codeshaping.DeadCodeCandidateSchedule

// deadCodeCandidateQuery aliases the leaf-owned candidate page query so the
// staying dead-code scan, the lane-B content reader
// (content_reader_dead_code_candidates.go), and the dead-code tests keep
// their signatures and literals unchanged. Delete with the #6060 big-bang
// handler move of code_dead_code_scan.go,
// code_dead_code_investigation.go, code_dead_code_cross_repo.go, and
// content_reader_dead_code_candidates.go.
type deadCodeCandidateQuery = codeshaping.DeadCodeCandidateQuery

// deadCodeCandidateContentStore aliases the leaf-owned candidate backend
// port so the staying dead-code scan keeps its read-model assertion
// unchanged. Delete with the #6060 big-bang handler move of
// code_dead_code_scan.go.
type deadCodeCandidateContentStore = codeshaping.DeadCodeCandidateContentStore

// deadCodeDefaultLimit aliases the leaf-owned display-limit default so the
// staying dead-code orchestrator, investigation, and cross-repo readers
// keep their clamps unchanged. Delete with the #6060 big-bang handler
// move of code_dead_code.go, code_dead_code_investigation.go, and
// code_dead_code_cross_repo.go.
const deadCodeDefaultLimit = codeshaping.DeadCodeDefaultLimit

// deadCodeCandidateQueryMin aliases the leaf-owned page-budget floor so the
// staying lane-B content reader and dead-code tests keep binding through
// it. Delete with the #6060 big-bang handler move of
// content_reader_dead_code_candidates.go and code_dead_code_scan.go.
const deadCodeCandidateQueryMin = codeshaping.DeadCodeCandidateQueryMin

// deadCodeCandidateQueryMax aliases the leaf-owned page-budget cap so the
// staying dead-code tests keep binding through it. Delete with the #6060
// big-bang handler move of code_dead_code_scan.go,
// code_dead_code_investigation.go, and code_dead_code_cross_repo.go.
const deadCodeCandidateQueryMax = codeshaping.DeadCodeCandidateQueryMax

// newDeadCodeCandidateSchedule forwards to the leaf-owned constructor so the
// staying dead-code scan, investigation, and cross-repo readers keep their
// call sites unchanged. Delete with the #6060 big-bang handler move of
// code_dead_code_scan.go, code_dead_code_investigation.go, and
// code_dead_code_cross_repo.go.
func newDeadCodeCandidateSchedule(labels []string, pageLimit int, totalLimit int) *deadCodeCandidateSchedule {
	return codeshaping.NewDeadCodeCandidateSchedule(labels, pageLimit, totalLimit)
}

// deadCodeCandidateQueryLimit forwards to the leaf-owned page-budget shaper
// so the staying dead-code scan, investigation, cross-repo readers, and
// tests keep their call sites unchanged. Delete with the #6060 big-bang
// handler move of code_dead_code_scan.go,
// code_dead_code_investigation.go, and code_dead_code_cross_repo.go.
func deadCodeCandidateQueryLimit(displayLimit int) int {
	return codeshaping.DeadCodeCandidateQueryLimit(displayLimit)
}

// deadCodeCandidateScanLimit forwards to the leaf-owned scan-ceiling shaper
// so the staying dead-code scan, investigation, cross-repo readers, and
// tests keep their call sites unchanged. Delete with the #6060 big-bang
// handler move of code_dead_code_scan.go,
// code_dead_code_investigation.go, and code_dead_code_cross_repo.go.
func deadCodeCandidateScanLimit(displayLimit int) int {
	return codeshaping.DeadCodeCandidateScanLimit(displayLimit)
}

// relationshipStoryApplyTokenBudget forwards to the leaf-owned budget
// applier so the staying story handler keeps its call site unchanged.
// Delete with the #6060 big-bang handler move of
// code_relationship_story.go.
func relationshipStoryApplyTokenBudget(req relationshipStoryRequest, rows *[]map[string]any) map[string]any {
	return codeshaping.RelationshipStoryApplyTokenBudget(req, rows)
}

// relationshipStoryRankByCentrality forwards to the leaf-owned centrality
// ranker so the staying story handler keeps its call site unchanged.
// Delete with the #6060 big-bang handler move of
// code_relationship_story.go.
func relationshipStoryRankByCentrality(rows []map[string]any) []map[string]any {
	return codeshaping.RelationshipStoryRankByCentrality(rows)
}

// relationshipStoryRankBasis aliases the leaf-owned ranking label so the
// staying story handler keeps its coverage stamp unchanged. Delete with the
// #6060 big-bang handler move of code_relationship_story.go.
const relationshipStoryRankBasis = codeshaping.RelationshipStoryRankBasis
