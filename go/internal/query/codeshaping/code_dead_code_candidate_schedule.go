// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeshaping

import "context"

// Dead-code candidate scan bounds. These moved with the schedule out of root
// package query's code_dead_code.go (#6060 lane A L3); root's
// family_code_shim_shaping.go aliases back the three the staying dead-code
// handlers, readers, and tests still name (DeadCodeDefaultLimit,
// DeadCodeCandidateQueryMin, DeadCodeCandidateQueryMax). The multiplier and
// the scan-multiple stay leaf-private: only the limit shapers below read
// them.
const (
	// DeadCodeDefaultLimit is the display limit when the caller names none.
	DeadCodeDefaultLimit = 100

	// deadCodeCandidateQueryMultiplier fans the display limit out to the
	// per-label candidate page budget.
	deadCodeCandidateQueryMultiplier = 10
	// DeadCodeCandidateQueryMin floors the per-label candidate page budget.
	DeadCodeCandidateQueryMin = 100
	// DeadCodeCandidateQueryMax caps the per-label candidate page budget.
	DeadCodeCandidateQueryMax = 250
	// deadCodeCandidateScanMaxPages caps the label round-robin scan as a
	// multiple of one page budget.
	deadCodeCandidateScanMaxPages = 10
)

// DeadCodeCandidatePage is one label-scoped page in the shared candidate scan.
type DeadCodeCandidatePage struct {
	Label  string
	Limit  int
	Offset int
	index  int
}

type deadCodeCandidateLabelCursor struct {
	label     string
	offset    int
	exhausted bool
}

// DeadCodeCandidateSchedule round-robins candidate labels under one global
// row ceiling. Sparse labels are retired after their first short page;
// saturated labels keep taking turns until the shared budget is exhausted.
// This preserves later-label fairness without multiplying downstream
// hydration and reachability work by the number of labels.
//
// It moved out of root package query (#6060 lane A L3); the staying
// dead-code readers name the exported methods directly, and construct it
// through root's family_code_shim_shaping.go forwarder.
type DeadCodeCandidateSchedule struct {
	cursors   []deadCodeCandidateLabelCursor
	pageLimit int
	remaining int
	next      int
	truncated bool
}

// NewDeadCodeCandidateSchedule starts a round-robin over labels with one
// page budget of pageLimit rows and a shared ceiling of totalLimit rows.
func NewDeadCodeCandidateSchedule(labels []string, pageLimit int, totalLimit int) *DeadCodeCandidateSchedule {
	cursors := make([]deadCodeCandidateLabelCursor, 0, len(labels))
	for _, label := range labels {
		cursors = append(cursors, deadCodeCandidateLabelCursor{label: label})
	}
	return &DeadCodeCandidateSchedule{
		cursors:   cursors,
		pageLimit: pageLimit,
		remaining: totalLimit,
	}
}

// NextPage deals the next label's page, skipping retired labels.
func (s *DeadCodeCandidateSchedule) NextPage() (DeadCodeCandidatePage, bool) {
	if s == nil || s.remaining <= 0 || len(s.cursors) == 0 {
		return DeadCodeCandidatePage{}, false
	}
	for checked := 0; checked < len(s.cursors); checked++ {
		index := s.next % len(s.cursors)
		s.next = (index + 1) % len(s.cursors)
		cursor := &s.cursors[index]
		if cursor.exhausted {
			continue
		}
		limit := min(s.pageLimit, s.remaining)
		return DeadCodeCandidatePage{
			Label:  cursor.label,
			Limit:  limit,
			Offset: cursor.offset,
			index:  index,
		}, true
	}
	return DeadCodeCandidatePage{}, false
}

// Record advances the page's cursor and retires sparse labels.
func (s *DeadCodeCandidateSchedule) Record(page DeadCodeCandidatePage, rowCount int) {
	if s == nil || page.index < 0 || page.index >= len(s.cursors) {
		return
	}
	cursor := &s.cursors[page.index]
	cursor.offset += rowCount
	s.remaining -= rowCount
	if rowCount < page.Limit {
		cursor.exhausted = true
	}
	if s.remaining <= 0 && rowCount == page.Limit {
		s.truncated = true
	}
}

// CandidateScanTruncated reports whether the shared budget cut the scan off
// on a full page.
func (s *DeadCodeCandidateSchedule) CandidateScanTruncated() bool {
	return s != nil && s.truncated
}

// DeadCodeCandidateQuery is one bounded candidate page for a single label,
// shared by the content read model and the graph fallback so both backends
// take the same scope, paging, and grant inputs.
type DeadCodeCandidateQuery struct {
	// RepoID anchors the scan to one repository when the caller named one.
	RepoID string
	// Label is the candidate node label / content entity type to scan.
	Label string
	// Language, when set, narrows the scan to one source language.
	Language string
	// Limit and Offset page the scan.
	Limit  int
	Offset int
	// AllowedRepositoryIDs restricts a corpus-wide (RepoID == "") scan to the
	// caller's granted repositories inside the query itself, so the page is
	// taken from the granted set rather than from a cross-tenant-polluted one.
	// It is never populated from a request body: deadCodeCandidateRows fills it
	// from the caller's AuthContext grant. Empty leaves the scan unrestricted,
	// which is what an unscoped caller wants; a grantless scoped caller never
	// reaches either backend.
	AllowedRepositoryIDs []string
}

// DeadCodeCandidateContentStore is the content read-model backend for one
// candidate page. The staying ContentReader implements it; the graph
// fallback covers readers that do not.
type DeadCodeCandidateContentStore interface {
	DeadCodeCandidateRows(ctx context.Context, query DeadCodeCandidateQuery) ([]map[string]any, error)
}

// DeadCodeCandidateQueryLimit fans a display limit out to the per-label
// candidate page budget, clamped into [DeadCodeCandidateQueryMin,
// DeadCodeCandidateQueryMax].
func DeadCodeCandidateQueryLimit(displayLimit int) int {
	if displayLimit <= 0 {
		displayLimit = DeadCodeDefaultLimit
	}
	candidateLimit := displayLimit*deadCodeCandidateQueryMultiplier + 1
	if candidateLimit < displayLimit+1 {
		return displayLimit + 1
	}
	if candidateLimit < DeadCodeCandidateQueryMin {
		return DeadCodeCandidateQueryMin
	}
	if candidateLimit > DeadCodeCandidateQueryMax {
		return DeadCodeCandidateQueryMax
	}
	return candidateLimit
}

// DeadCodeCandidateScanLimit scales one page budget to the shared
// multi-label scan ceiling.
func DeadCodeCandidateScanLimit(displayLimit int) int {
	return DeadCodeCandidateQueryLimit(displayLimit) * deadCodeCandidateScanMaxPages
}
