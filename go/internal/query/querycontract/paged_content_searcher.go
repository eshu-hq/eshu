// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "context"

// PagedContentSearcher is implemented by content stores that can push offset
// and multi-repo scope into SQL instead of paginating in handler memory.
//
// It lives in querycontract (rather than beside its callers) so the
// contentread handler family (#6060 lane B1) can assert it from another
// package: Go qualifies an unexported method name by its declaring package
// for interface satisfaction, and the request type the old root-local
// interface took is likewise unexported, so a root-local interface could
// never be satisfied across the package boundary -- only silently missed,
// falling back to the slower per-request search loop. Decomposed primitive
// parameters keep every signature element exported, so root's ContentReader
// still satisfies this interface while living in a different package than
// the handlers that assert it.
//
// Exactly one of repoID / repoIDs selects the scope: a non-empty repoID
// searches one repository, a non-empty repoIDs searches that explicit set,
// and both empty searches all indexed repositories. limit carries the
// caller's probe row (request limit + 1); offset is the zero-based SQL
// offset.
type PagedContentSearcher interface {
	SearchFiles(ctx context.Context, repoID string, repoIDs []string, pattern string, limit, offset int) ([]FileContent, error)
	SearchEntities(ctx context.Context, repoID string, repoIDs []string, pattern string, limit, offset int) ([]EntityContent, error)
}

// Partial-search reasons carried in SearchPartial.Reason and in the truth
// envelope's reason when a budgeted search ends before it can prove the page.
const (
	// SearchPartialCandidateBudgetExceeded means the work budget ran out before
	// the page was complete and no statement overran its own timeout by more
	// than SearchLargeDocumentOverrunMS.
	SearchPartialCandidateBudgetExceeded = "candidate_budget_exceeded"
	// SearchPartialBudgetExceededOnLargeDocument means a cancelled statement
	// ran past its timeout by more than SearchLargeDocumentOverrunMS: one
	// candidate's recheck cannot be interrupted, so a large document is the
	// likely cause.
	SearchPartialBudgetExceededOnLargeDocument = "budget_exceeded_on_large_document"
	// SearchLargeDocumentOverrunMS is the measured overrun above which a
	// partial result names a large document as the cause.
	SearchLargeDocumentOverrunMS = 100
	// SearchPartialHint tells the caller how to turn a partial result into an
	// exact one.
	SearchPartialHint = "add repo_id to scope the search"
)

// SearchCursor names the last key a budgeted search visited, in repo_id then
// relative_path order. A search resumed from it starts strictly after it.
type SearchCursor struct {
	RepoID       string `json:"repo_id"`
	RelativePath string `json:"relative_path"`
}

// IsZero reports whether the cursor names no key, which starts a search at the
// first row.
func (c SearchCursor) IsZero() bool {
	return c.RepoID == "" && c.RelativePath == ""
}

// SearchPartial is the explicit incomplete-answer marker a budgeted search
// returns instead of an error when its work budget runs out. It travels in the
// response body next to truncated=true; it never replaces the rows found.
type SearchPartial struct {
	// Reason is SearchPartialCandidateBudgetExceeded or
	// SearchPartialBudgetExceededOnLargeDocument.
	Reason string `json:"reason"`
	// RowsScannedInOrder counts the rows this call visited in key order, up to
	// and including Cursor; a resumed call counts from its request cursor.
	RowsScannedInOrder int `json:"rows_scanned_in_order"`
	// RowsMatched counts the matching rows found so far in key order, before
	// the request offset is applied. A client that resumes from Cursor sends
	// offset max(0, offset - RowsMatched).
	RowsMatched int `json:"rows_matched"`
	// Cursor is the last visited key; empty when no step completed.
	Cursor SearchCursor `json:"cursor"`
	// BudgetMS is the requested work budget.
	BudgetMS int64 `json:"budget_ms"`
	// ElapsedMS is the SQL wall time the server measured.
	ElapsedMS int64 `json:"elapsed_ms"`
	// OverrunMS is how far a cancelled statement ran past its own timeout; 0
	// when none did.
	OverrunMS int64 `json:"overrun_ms"`
	// Hint tells the caller how to get an exact answer.
	Hint string `json:"hint"`
}

// FileSearchPage is one page of an unscoped file-content search. Files holds
// the page rows in repo_id, relative_path order. More reports, for a complete
// answer, that a row exists past the page. Partial is non-nil when the budget
// ended the search first; the page is then a prefix of the exact answer.
type FileSearchPage struct {
	Files   []FileContent
	More    bool
	Partial *SearchPartial
}

// UnscopedFileSearcher is implemented by content stores that answer a file
// search with no repository filter inside a bounded work budget, returning a
// partial page with a cursor instead of an unbounded scan. The parameters are
// decomposed primitives for the same reason as PagedContentSearcher: an
// unexported type in the signature would silently break interface
// satisfaction across packages. limit is the page size; the store reads its
// own look-ahead row. A non-empty cursor starts the search after that key.
type UnscopedFileSearcher interface {
	SearchFilesUnscoped(ctx context.Context, pattern string, limit, offset int, cursorRepoID, cursorPath string) (FileSearchPage, error)
}
