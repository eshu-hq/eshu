// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	payloadstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The paged read of the supports table by key (#10880 PR-B2c, ruling
// arbiter-hotkey-page.md, option D). A reducer reader drains every support row
// that matches a set of keys, one page per statement:
//
//   - Each arm is a per-key ordered LIMIT scan on the arm's own partial btree
//     (column, fact_id): `unnest($n) AS k(v) CROSS JOIN LATERAL (SELECT s.fact_id
//     FROM supports s WHERE s.col = k.v [AND s.fact_id > $cursor] ORDER BY
//     s.fact_id LIMIT N)`. The LIMIT is the fence: nothing about the scope tables
//     appears inside it, so the planner cannot flip to one probe per active scope
//     and every page stops after N index entries per key.
//   - The arms are UNION ALL, cut to the first len(arms)*N ids in fact_id order
//     (a support row matches at most once per arm), made distinct, and cut to N.
//     These N ids are the RAW page.
//   - The outer query joins the raw ids to the supports by primary key, LEFT
//     JOINs the two scope tables on their keys, and returns every raw row in
//     fact_id order with an is_active flag and the payload only when active.
//
// The reader appends only active rows, advances its cursor on the last RAW fact
// id, and stops only when the raw page holds fewer than N rows, so a page full
// of superseded-generation rows never reads as the end of the drain. Each page is
// its own autocommit statement: no cursor and no transaction stays open between
// pages. At a package with 1,600,000 supports a page reads 5,958 buffers and
// takes 3.8 ms warm, with no temp, under both plan_cache_mode settings
// (docs/internal/evidence/10880-b2c/reader_plans.md).

// supplyChainImpactSupportsPackageIndexPredicate is the partial-index predicate
// of the supports table's package_id btree ((package_id, fact_id) WHERE
// package_id <> ”, migration 345, #10880 PR-B2c). The package arm of a supports
// page repeats it as a literal guard so the planner can prove the partial index
// usable; TestSupplyChainImpactSupportsPackageIndexMatchesTheMigration derives
// the migration and the arm from this one constant.
const supplyChainImpactSupportsPackageIndexPredicate = `package_id <> ''`

// supplyChainImpactSupportsRepositoryIndexPredicate is the partial-index predicate
// of the supports table's repository btree ((repository_id, fact_id) WHERE
// repository_id <> ”, migration 346, #10880 PR-B2c), repeated as the repository
// arm's literal guard the way the package arm repeats the package predicate.
// TestSupplyChainImpactSupportsRepositoryIndexMatchesTheMigration derives the
// migration and the arm from this one constant.
const supplyChainImpactSupportsRepositoryIndexPredicate = `repository_id <> ''`

// supplyChainImpactSupportsArm names one filter arm of a supports page read.
type supplyChainImpactSupportsArm struct {
	// column is the supports column the arm matches a key against.
	column string
	// guard is a literal predicate repeated in the arm so a partial index on the
	// column can be proven usable, or "" for an unconditional index.
	guard string
	// param is the $n position of the arm's key array.
	param int
}

// supplyChainImpactSupportsDrainLogRows is the raw-row count at which a drain
// logs one structured line, so a hot key is findable without a dashboard.
const supplyChainImpactSupportsDrainLogRows = 100000

// supplyChainImpactSupportsPageQuery renders the page statement for arms. A
// cursorParam of zero renders the first-page text, which carries no cursor
// predicate; a positive cursorParam renders the next-page text with a plain
// `s.fact_id > $cursorParam`. The page size is rendered as a literal so a
// generic plan sees it, and the cursor is never written as `$n = ” OR ...`,
// which a generic plan could only treat as a filter.
func supplyChainImpactSupportsPageQuery(arms []supplyChainImpactSupportsArm, cursorParam int) string {
	pageSize := strconv.Itoa(listFactsByKindPageSize)
	cursor := ""
	if cursorParam > 0 {
		cursor = " AND s.fact_id > $" + strconv.Itoa(cursorParam)
	}
	rendered := make([]string, 0, len(arms))
	for _, arm := range arms {
		guard := ""
		if arm.guard != "" {
			guard = " AND s." + arm.guard
		}
		rendered = append(rendered, "SELECT x.fact_id FROM unnest($"+strconv.Itoa(arm.param)+"::text[]) AS k(v) CROSS JOIN LATERAL (\n"+
			"        SELECT s.fact_id FROM supply_chain_impact_finding_supports AS s\n"+
			"        WHERE s."+arm.column+" = k.v"+guard+cursor+"\n"+
			"        ORDER BY s.fact_id LIMIT "+pageSize+") x")
	}
	return `WITH raw AS (
    SELECT d.fact_id FROM (
        SELECT DISTINCT t.fact_id FROM (
            SELECT a.fact_id FROM (
` + strings.Join(rendered, "\n            UNION ALL\n") + `
            ) AS a ORDER BY a.fact_id LIMIT ` + strconv.Itoa(len(arms)*listFactsByKindPageSize) + `
        ) AS t
    ) AS d ORDER BY d.fact_id LIMIT ` + pageSize + `
)
SELECT r.fact_id, s.scope_id, s.generation_id, s.source_confidence,
       (sc.scope_id IS NOT NULL AND g.generation_id IS NOT NULL) AS is_active,
       CASE WHEN sc.scope_id IS NOT NULL AND g.generation_id IS NOT NULL THEN s.payload END AS payload
FROM raw AS r
JOIN supply_chain_impact_finding_supports AS s ON s.fact_id = r.fact_id
LEFT JOIN ingestion_scopes AS sc ON sc.scope_id = s.scope_id AND sc.active_generation_id = s.generation_id
LEFT JOIN scope_generations AS g ON g.scope_id = s.scope_id AND g.generation_id = s.generation_id AND g.status = 'active'
ORDER BY r.fact_id
`
}

// supplyChainImpactSupportsDrain is one reader's drain of the supports table:
// the arms it filters on and the key arrays bound to them, in $n order.
type supplyChainImpactSupportsDrain struct {
	reader      string
	arms        []supplyChainImpactSupportsArm
	firstQuery  string
	nextQuery   string
	keyArgs     []any
	instruments *telemetry.Instruments
	// afterPage, when set, runs after each page is read and before the next one
	// starts. It is a test seam for the concurrency proof; production leaves it nil.
	afterPage func(page int)
}

// newSupplyChainImpactSupportsDrain prepares a drain whose arms bind keyArgs, one
// array per arm in order; the cursor is the next parameter after them.
func newSupplyChainImpactSupportsDrain(
	instruments *telemetry.Instruments,
	reader string,
	arms []supplyChainImpactSupportsArm,
	keyArgs ...any,
) supplyChainImpactSupportsDrain {
	return supplyChainImpactSupportsDrain{
		reader:      reader,
		arms:        arms,
		firstQuery:  supplyChainImpactSupportsPageQuery(arms, 0),
		nextQuery:   supplyChainImpactSupportsPageQuery(arms, len(keyArgs)+1),
		keyArgs:     keyArgs,
		instruments: instruments,
	}
}

// run drains every page and returns the active supports as impact-kind
// envelopes in fact_id order. The cursor advances on the last raw id of each page
// and the drain stops on a raw page shorter than the page size.
func (d supplyChainImpactSupportsDrain) run(ctx context.Context, queryer db.Queryer) ([]facts.Envelope, error) {
	started := time.Now()
	var loaded []facts.Envelope
	var cursorFactID string
	pages, rawRows := 0, 0
	for {
		query, args := d.firstQuery, d.keyArgs
		if cursorFactID != "" {
			query, args = d.nextQuery, append(append(make([]any, 0, len(d.keyArgs)+1), d.keyArgs...), cursorFactID)
		}
		pageRaw, lastRawID, active, err := d.page(ctx, queryer, query, args)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, active...)
		pages++
		rawRows += pageRaw
		if d.afterPage != nil {
			d.afterPage(pages)
		}
		if pageRaw < listFactsByKindPageSize {
			break
		}
		cursorFactID = lastRawID
	}
	elapsed := time.Since(started)
	d.instruments.RecordSupplyChainImpactSupportsDrainRawRows(ctx, d.reader, int64(rawRows))
	d.instruments.RecordSupplyChainImpactSupportsDrainDuration(ctx, d.reader, elapsed)
	if rawRows >= supplyChainImpactSupportsDrainLogRows {
		log.Printf("event_name=supply_chain_impact_supports.large_drain reader=%s keys=%d pages=%d raw_rows=%d active_rows=%d duration_ms=%d",
			d.reader, d.keyCount(), pages, rawRows, len(loaded), elapsed.Milliseconds())
	}
	return loaded, nil
}

// keyCount is the number of keys the drain filters on, summed over its arms.
func (d supplyChainImpactSupportsDrain) keyCount() int {
	count := 0
	for _, arg := range d.keyArgs {
		if keys, ok := arg.([]string); ok {
			count += len(keys)
		}
	}
	return count
}

// page reads one page statement and returns the raw row count, the last raw
// fact id, and the active rows as envelopes.
func (d supplyChainImpactSupportsDrain) page(
	ctx context.Context,
	queryer db.Queryer,
	query string,
	args []any,
) (int, string, []facts.Envelope, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, "", nil, fmt.Errorf("read supply chain impact supports page: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var active []facts.Envelope
	raw := 0
	lastRaw := ""
	for rows.Next() {
		var factID, scopeID, generationID, sourceConfidence string
		var isActive bool
		var rawPayload []byte
		if err := rows.Scan(&factID, &scopeID, &generationID, &sourceConfidence, &isActive, &rawPayload); err != nil {
			return 0, "", nil, fmt.Errorf("scan supply chain impact supports page: %w", err)
		}
		raw++
		lastRaw = factID
		if !isActive {
			continue
		}
		payload, err := payloadstore.UnmarshalPayload(rawPayload)
		if err != nil {
			return 0, "", nil, fmt.Errorf("decode supply chain impact support payload: %w", err)
		}
		active = append(active, facts.Envelope{
			FactID:           factID,
			ScopeID:          scopeID,
			GenerationID:     generationID,
			FactKind:         facts.ReducerSupplyChainImpactFindingFactKind,
			SourceConfidence: sourceConfidence,
			Payload:          payload,
		})
	}
	if err := rows.Err(); err != nil {
		return 0, "", nil, fmt.Errorf("read supply chain impact supports page: %w", err)
	}
	return raw, lastRaw, active, nil
}
