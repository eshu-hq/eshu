// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"context"
	"math"
	"path/filepath"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
)

// goldenCase is one search whose full observable output the golden pins.
type goldenCase struct {
	name          string
	corpus        func() *fakeCorpus
	budget        time.Duration
	limit, offset int
	cursor        querycontract.SearchCursor
}

// fileGoldenRoute is the content_files route through Searcher.Search.
func fileGoldenRoute() goldenRoute {
	return goldenRoute{
		name:         "file",
		path:         filepath.Join("testdata", "file_route.golden.json"),
		statementSet: unscoped.Statements(),
		cases:        fileGoldenCases(),
		search: func(ctx context.Context, s *unscoped.Searcher, tc goldenCase) (goldenResult, error) {
			page, err := s.Search(ctx, "needle", tc.limit, tc.offset, tc.cursor)
			if err != nil {
				return goldenResult{}, err
			}
			rows, err := marshalRows(page.Files)
			return goldenResult{rows: rows, more: page.More, partial: page.Partial}, err
		},
	}
}

// goldenEntityRow is how the entity golden renders one hit: the entity and
// its raw metadata as text, so the bytes the query layer decodes are visible.
type goldenEntityRow struct {
	Entity      querycontract.EntityContent `json:"entity"`
	MetadataRaw string                      `json:"metadata_raw"`
}

// entityGoldenRoute is the content_entities route through
// Searcher.SearchEntities.
func entityGoldenRoute() goldenRoute {
	return goldenRoute{
		name:         "entity",
		path:         filepath.Join("testdata", "entity_route.golden.json"),
		statementSet: unscoped.EntityStatements(),
		cases:        entityGoldenCases(),
		entities:     true,
		search: func(ctx context.Context, s *unscoped.Searcher, tc goldenCase) (goldenResult, error) {
			page, err := s.SearchEntities(ctx, "needle", tc.limit, tc.offset, tc.cursor)
			if err != nil {
				return goldenResult{}, err
			}
			hits := make([]goldenEntityRow, 0, len(page.Hits))
			for _, hit := range page.Hits {
				hits = append(hits, goldenEntityRow{Entity: hit.Entity, MetadataRaw: string(hit.MetadataRaw)})
			}
			rows, err := marshalRows(hits)
			return goldenResult{rows: rows, more: page.More, partial: page.Partial}, err
		},
	}
}

// fileGoldenCases cover every outcome and both partial reasons, a resumed
// call, a non-default budget, a non-zero offset, and the tail cap and floor.
func fileGoldenCases() []goldenCase {
	resumeAt := querycontract.SearchCursor{RepoID: "repo-010", RelativePath: "file-01099.go"}
	return []goldenCase{
		{name: "exact in the probe", corpus: func() *fakeCorpus {
			return newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i%10 == 0 })
		}, limit: 5, offset: 2},
		// Explicit 800 ms: at the 2000 ms default the walk reaches row
		// 15000 itself, so the tail-exhaustion path this case pins
		// would no longer trigger (#9306).
		{name: "exhausted by the tail", corpus: func() *fakeCorpus {
			corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i == 15000 })
			corpus.tailCost = func(int) time.Duration { return 150 * ms }
			return corpus
		}, budget: 800 * ms, limit: 50},
		{name: "exhausted inside the probe window", corpus: func() *fakeCorpus {
			return newCorpus(150, 50*time.Microsecond, func(i int) bool { return i == 7 || i == 140 })
		}, limit: 10},
		// Both cases below run at an explicit 800 ms: at the 2000 ms
		// default the walk fills the page, so the large-document and
		// resume partials they pin would no longer trigger (#9306).
		{name: "partial on a large document", corpus: func() *fakeCorpus { return sparsePartialCorpus(120 * ms) }, budget: 800 * ms, limit: 200},
		{
			name: "partial resumed from a cursor", corpus: func() *fakeCorpus { return sparsePartialCorpus(3 * ms) },
			budget: 800 * ms, limit: 200, offset: 4, cursor: resumeAt,
		},
		{name: "partial without progress", corpus: stalledCorpus, limit: 50},
		{
			name: "partial at a 400 ms budget", corpus: func() *fakeCorpus { return sparsePartialCorpus(2 * ms) },
			budget: 400 * ms, limit: 30,
		},
		// The three cases below pin the tail figures. The probe costs the
		// stated time and the continuation is predicted too dear for its
		// room, so the tail meets the budget left after the probe alone.
		// Stated at the 2000 ms default (#9306): the floor cases leave
		// exactly the 375 ms tail floor, and one millisecond under it.
		// Probe 600 ms at the 2000 ms default (#9306): the first
		// continuation step is predicted at 2.5 x 600 = 1500 ms, too
		// dear for the 1400 ms left, so the tail binds at the cap.
		{name: "tail timeout binds at the tail cap", corpus: func() *fakeCorpus {
			return slowProbeCorpus(600*ms, 100*ms)
		}, limit: 5},
		{name: "tail runs with exactly the tail floor left", corpus: func() *fakeCorpus {
			return slowProbeCorpus(1625*ms, 100*ms)
		}, limit: 5},
		{name: "tail skipped one millisecond under the tail floor", corpus: func() *fakeCorpus {
			return slowProbeCorpus(1626*ms, 100*ms)
		}, limit: 5},
	}
}

// entityGoldenCases mirror the file cases on content_entities, then add the
// cases only a four-part cursor has.
func entityGoldenCases() []goldenCase {
	shared := newEntityCorpus(9, 50*time.Microsecond, func(int) bool { return true })
	last := shared.entities[len(shared.entities)-1]
	return []goldenCase{
		{name: "exact in the probe", corpus: func() *fakeCorpus {
			return newEntityCorpus(20000, 50*time.Microsecond, func(i int) bool { return i%10 == 0 })
		}, limit: 5, offset: 2},
		{name: "exhausted by the tail", corpus: func() *fakeCorpus {
			corpus := newEntityCorpus(20000, 50*time.Microsecond, func(i int) bool { return i == 15000 })
			corpus.tailCost = func(int) time.Duration { return 150 * ms }
			return corpus
		}, limit: 50},
		{name: "exhausted inside the probe window", corpus: func() *fakeCorpus {
			return newEntityCorpus(150, 50*time.Microsecond, func(i int) bool { return i == 7 || i == 140 })
		}, limit: 10},
		{name: "partial on a large document", corpus: func() *fakeCorpus { return sparseEntityPartialCorpus(120 * ms) }, limit: 200},
		// Explicit 800 ms like the file resume case: at the 2000 ms
		// default the walk fills the page and the resume partial no
		// longer triggers (#9306).
		{
			name: "partial resumed from a cursor", corpus: func() *fakeCorpus { return sparseEntityPartialCorpus(3 * ms) },
			budget: 800 * ms, limit: 200, offset: 4, cursor: entityCursorAt(3299),
		},
		{name: "partial without progress", corpus: stalledEntityCorpus, limit: 50},
		{
			name: "partial at a 400 ms budget", corpus: func() *fakeCorpus { return sparseEntityPartialCorpus(2 * ms) },
			budget: 400 * ms, limit: 30,
		},
		// The entity tail cap (1600 ms) binds only when more than 1600 ms are
		// left after the probe, so this probe costs 100 ms.
		{name: "tail timeout binds at the tail cap", corpus: func() *fakeCorpus {
			return slowEntityProbeCorpus(100*ms, 100*ms)
		}, limit: 5},
		{name: "tail runs with exactly the tail floor left", corpus: func() *fakeCorpus {
			return slowEntityProbeCorpus(1625*ms, 100*ms)
		}, limit: 5},
		{name: "tail skipped one millisecond under the tail floor", corpus: func() *fakeCorpus {
			return slowEntityProbeCorpus(1626*ms, 100*ms)
		}, limit: 5},
		// Rows 3, 4 and 5 share one (repo_id, relative_path); resuming after
		// row 4 must return row 5 and not row 3 or 4.
		{name: "resume between two entities of one file", corpus: func() *fakeCorpus {
			return newEntityCorpus(9, 50*time.Microsecond, func(int) bool { return true })
		}, limit: 10, cursor: entityCursorAt(4)},
		// A start_line past the int32 column range binds saturated.
		{name: "cursor start_line saturates at the int32 range", corpus: func() *fakeCorpus {
			return newEntityCorpus(9, 50*time.Microsecond, func(int) bool { return true })
		}, limit: 10, cursor: querycontract.SearchCursor{
			RepoID: last.repo, RelativePath: last.path, StartLine: math.MaxInt64, EntityID: "zzz-no-such-entity",
		}},
		// A stalled call returns the request cursor; its start_line of zero is
		// omitted on the wire and binds as zero.
		{name: "cursor with a zero start_line round trips", corpus: stalledEntityCorpus, limit: 50, cursor: querycontract.SearchCursor{
			RepoID: "repo-000", RelativePath: "file-00000.go", StartLine: 0, EntityID: "entity-000000",
		}},
	}
}

// slowProbeCorpus has one match, far past the probe window, and a probe
// window that costs probeCost in total. The first continuation step is then
// predicted at 2.5 times the probe, which exceeds the phase room, so the walk
// goes from the probe straight to the tail decision with budget - probeCost
// left. tailCost is the tail's simulated wall.
func slowProbeCorpus(probeCost, tailCost time.Duration) *fakeCorpus {
	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i == 15000 })
	const probeRows = 200
	each := probeCost / probeRows
	for i := 0; i < probeRows; i++ {
		corpus.rows[i].cost = each
	}
	corpus.rows[probeRows-1].cost += probeCost - each*probeRows
	corpus.tailCost = func(int) time.Duration { return tailCost }
	return corpus
}

// slowEntityProbeCorpus is slowProbeCorpus over content_entities, with the
// entity shape's 1,000-row probe.
func slowEntityProbeCorpus(probeCost, tailCost time.Duration) *fakeCorpus {
	corpus := newEntityCorpus(20000, 50*time.Microsecond, func(i int) bool { return i == 15000 })
	const probeRows = 1000
	each := probeCost / probeRows
	for i := 0; i < probeRows; i++ {
		corpus.entities[i].cost = each
	}
	corpus.entities[probeRows-1].cost += probeCost - each*probeRows
	corpus.tailCost = func(int) time.Duration { return tailCost }
	return corpus
}

// sparsePartialCorpus is the dense-late class of partialCorpus with one match
// per 200 rows, so a partial page stays short; lag is how far the cancelled
// tail runs past its timeout.
func sparsePartialCorpus(lag time.Duration) *fakeCorpus {
	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i >= 3000 && i%200 == 0 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	corpus.tailLag = lag
	return corpus
}

// sparseEntityPartialCorpus is sparsePartialCorpus over content_entities.
func sparseEntityPartialCorpus(lag time.Duration) *fakeCorpus {
	corpus := newEntityCorpus(20000, 50*time.Microsecond, func(i int) bool { return i >= 3000 && i%200 == 0 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	corpus.tailLag = lag
	return corpus
}

// stalledEntityCorpus is stalledCorpus over content_entities: the probe costs
// more than the whole budget, so no window completes.
func stalledEntityCorpus() *fakeCorpus {
	corpus := newEntityCorpus(20000, 5*ms, func(i int) bool { return i == 15000 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	return corpus
}

// entityCursorAt is the four-part key of entity row i of newEntityCorpus.
func entityCursorAt(i int) querycontract.SearchCursor {
	row := newEntityCorpus(i+1, 0, func(int) bool { return false }).entities[i]
	return querycontract.SearchCursor{RepoID: row.repo, RelativePath: row.path, StartLine: row.start, EntityID: row.id}
}
