// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// MaxRefillReads caps how many tag-history windows ONE scoped request may read
// while refilling its page (#6564 review finding 1).
//
// Why a cap at all: refilling means reading further windows until `limit`
// visible rows are collected, and a caller whose grant covers a small slice of
// a heavily shared repository:tag could otherwise drive an unbounded scan from
// a single request.
//
// Why four: each window costs one keyset read plus one BuiltFromCypher lookup,
// and the lookup's measured worst case is the pathological one the refill loop
// actually triggers. A page whose digests have no ContainerImage node is
// exactly the fully-withheld page that refills, and 400 such missing keys cost
// 1135-1175 ms cold on a 5,000-image store and 2247 ms at 10,000
// (docs/internal/evidence/6564-tag-history-grant-binding.md). Four windows
// bound that at roughly 4.7 s and 9 s. The first still sits inside the handler
// histogram's 5 s top bucket; the second is honestly an outlier above it, and a
// larger cap multiplies it further, which is why this is four and not a round
// ten.
//
// Hitting the cap is never served as a complete page: Truncated stays true and
// the cursor resumes at a key the scan reached, so following it continues the
// same walk rather than repeating or skipping history.
const MaxRefillReads = 4

// ScopedPage is the outcome of refilling one grant-filtered page.
//
// NextKey is the continuation point, or nil when the history ended inside this
// page. It is a KEY, never a row position: the handler renders it with
// EncodeCursor, and the only reason it is not simply "the last row you were
// shown" is the fully-withheld capped page documented on RefillScopedPage.
type ScopedPage struct {
	Rows       []Row
	NextKey    *Key
	Truncated  bool
	CapReached bool
	Reads      int
	Counts     GrantCounts
}

// RefillScopedPage collects up to limit VISIBLE rows for a scoped caller,
// reading successive windows after the given key -- or from the start of the
// history when after is nil -- until the page is full, the history ends, or
// MaxRefillReads windows have been read.
//
// Every window uses the same two single-clause statements the unscoped path
// uses -- one of the keyset statements then BuiltFromCypher -- because the
// pinned NornicDB build returns zero rows for the multi-clause grant join this
// would otherwise be written as
// (docs/internal/evidence/6564-tag-history-grant-binding.md).
//
// # The window is MaxLimit-sized, not limit-sized
//
// That is deliberate and it is the half of this design that shrinks a
// disclosure rather than relocating it. The raw span one request scans is
// MaxRefillReads windows, so with limit-sized windows it was 4*limit raw rows:
// at limit=1 a caller receiving count 0 with truncated true had learned that
// FOUR specific consecutive observations were another tenant's, and walking at
// limit=1 mapped withheld rows at granularity four. Fixing the window at
// MaxLimit makes that span a constant 800 raw rows whatever limit is, so the
// same signal degrades to "at least 800 consecutive rows here are not yours" --
// reachable only on a tag whose history is very large and whose grant barely
// covers it. It costs a limit=1 scoped caller the 200-row read a limit=200
// caller already pays; that is the already-measured worst case, not a new one.
//
// # What the cursor names, and the one case it is not a row you saw
//
// Truncated is true exactly when raw history remains beyond the page, so a
// short page is either the genuine end of the visible history or an honest
// "there is more, keep following the cursor"; it is never a filtered-short page
// presented as complete.
//
//   - Page filled inside a window: NextKey is the key of the LAST ROW THE
//     CALLER RECEIVED. It discloses nothing -- the caller holds that row.
//   - Cap reached with at least one visible row: NextKey is the last visible
//     row's key, again a row the caller holds. The next request re-scans the
//     withheld rows after it and returns no duplicate, because no visible row
//     sits in that span. That re-scan costs UP TO MaxRefillReads windows, not
//     one: the last visible row can sit anywhere in the scan, including the
//     first window, so the withheld run after it can span every remaining
//     window and the next request re-reads all of it. The walk still advances
//     -- a request that sees no visible row at all resumes from the last RAW
//     row instead (next case), so the frontier always moves forward.
//   - Cap reached with ZERO visible rows: NextKey MUST be the last RAW row
//     scanned. A key at the last visible row would re-scan the same 800 rows
//     forever and the walk could never advance. That row may be withheld, which
//     is exactly why the token carrying it leaves the wire SEALED (see Cursor):
//     the caller can replay the frontier but cannot read it and cannot choose
//     one, so what such a page leaves behind is a count and never an identity.
//   - History ended: NextKey is nil and Truncated is false.
//
// # Why the walk terminates and never repeats a row
//
// Each iteration's next window starts after the last RAW row of the window just
// consumed, which is strictly greater in the total order than the window's own
// start key, and AfterKeyCypher/NullTailCypher are strict > on that order. So
// no window is re-read within a request, and across requests no visible row can
// be returned twice. A key whose row has since been retracted is harmless: the
// predicate is on values, not on that row still existing.
func RefillScopedPage(
	ctx context.Context,
	graph querycontract.GraphQuery,
	imageRef string,
	after *Key,
	limit int,
	access querycontract.RepositoryAccessFilter,
) (ScopedPage, error) {
	page := ScopedPage{Rows: make([]Row, 0, limit)}
	// lastRaw is the key of the last RAW row consumed, which is where the next
	// window starts; lastVisible is the key of the last row appended to the
	// page. They differ exactly when the window's tail was withheld.
	lastRaw := after
	var lastVisible *Key

	for page.Reads < MaxRefillReads {
		window, more, err := ReadWindow(ctx, graph, imageRef, lastRaw, MaxLimit)
		if err != nil {
			return page, err
		}
		page.Reads++
		if len(window) == 0 {
			return page, nil
		}

		edges := map[string][]string{}
		if digests := grantDigests(window); len(digests) > 0 {
			edges, err = LookupBuiltFromRepositories(ctx, graph, digests)
			if err != nil {
				// Fail closed: never fall back to the unfiltered window.
				return page, err
			}
		}

		for i := range window {
			kept, visible := grantDecision(window[i].Row, edges, access, &page.Counts)
			if !visible {
				continue
			}
			page.Rows = append(page.Rows, kept)
			lastVisible = &window[i].Key
			if len(page.Rows) < limit {
				continue
			}
			page.NextKey = &window[i].Key
			page.Truncated = more || i+1 < len(window)
			return page, nil
		}

		lastRaw = &window[len(window)-1].Key
		if !more {
			return page, nil
		}
	}

	// The cap stopped a scan that still has raw history ahead of it.
	page.Truncated = true
	page.CapReached = true
	page.NextKey = lastVisible
	if page.NextKey == nil {
		page.NextKey = lastRaw
	}
	return page, nil
}
