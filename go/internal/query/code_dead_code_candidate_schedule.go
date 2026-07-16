// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// deadCodeCandidatePage is one label-scoped page in the shared candidate scan.
type deadCodeCandidatePage struct {
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

// deadCodeCandidateSchedule round-robins candidate labels under one global row
// ceiling. Sparse labels are retired after their first short page; saturated
// labels keep taking turns until the shared budget is exhausted. This preserves
// later-label fairness without multiplying downstream hydration and
// reachability work by the number of labels.
type deadCodeCandidateSchedule struct {
	cursors   []deadCodeCandidateLabelCursor
	pageLimit int
	remaining int
	next      int
	truncated bool
}

func newDeadCodeCandidateSchedule(labels []string, pageLimit int, totalLimit int) *deadCodeCandidateSchedule {
	cursors := make([]deadCodeCandidateLabelCursor, 0, len(labels))
	for _, label := range labels {
		cursors = append(cursors, deadCodeCandidateLabelCursor{label: label})
	}
	return &deadCodeCandidateSchedule{
		cursors:   cursors,
		pageLimit: pageLimit,
		remaining: totalLimit,
	}
}

func (s *deadCodeCandidateSchedule) nextPage() (deadCodeCandidatePage, bool) {
	if s == nil || s.remaining <= 0 || len(s.cursors) == 0 {
		return deadCodeCandidatePage{}, false
	}
	for checked := 0; checked < len(s.cursors); checked++ {
		index := s.next % len(s.cursors)
		s.next = (index + 1) % len(s.cursors)
		cursor := &s.cursors[index]
		if cursor.exhausted {
			continue
		}
		limit := min(s.pageLimit, s.remaining)
		return deadCodeCandidatePage{
			Label:  cursor.label,
			Limit:  limit,
			Offset: cursor.offset,
			index:  index,
		}, true
	}
	return deadCodeCandidatePage{}, false
}

func (s *deadCodeCandidateSchedule) record(page deadCodeCandidatePage, rowCount int) {
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

func (s *deadCodeCandidateSchedule) candidateScanTruncated() bool {
	return s != nil && s.truncated
}
