// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scalars"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// activeWorkSummaryColumns are the only fact_work_items columns the five
// summary sections read, so the materialized active set stays small (#6794).
const activeWorkSummaryColumns = `work.work_item_id,
         work.scope_id,
         work.generation_id,
         work.stage,
         work.domain,
         work.status,
         work.conflict_domain,
         work.conflict_key,
         work.visible_at,
         work.claim_until,
         work.created_at,
         work.updated_at,
         work.payload,
         work.failure_class,
         work.failure_message,
         work.failure_details,
         work.provenance_edge_identity_upgrade_required`

// latestQueueFailureSelect lists active work items that are retrying, failed,
// or dead-lettered with failure text; the status surface keeps the newest one.
const latestQueueFailureSelect = `SELECT stage,
       domain,
       status,
       work_item_id,
       scope_id,
       generation_id,
       COALESCE(failure_class, '') AS failure_class,
       COALESCE(failure_message, '') AS failure_message,
       COALESCE(failure_details, '') AS failure_details,
       updated_at
FROM active_fact_work_items
WHERE status IN ('retrying', 'failed', 'dead_letter')
  AND (
    NULLIF(BTRIM(COALESCE(failure_class, '')), '') IS NOT NULL
    OR NULLIF(BTRIM(COALESCE(failure_message, '')), '') IS NOT NULL
    OR NULLIF(BTRIM(COALESCE(failure_details, '')), '') IS NOT NULL
  )`

// latestQueueFailureOrder is latestQueueFailureSelect's result order.
const latestQueueFailureOrder = `updated_at DESC, work_item_id ASC`

// Section names in activeWorkSummaryQuery's result stream.
const (
	activeWorkSectionStage    = "stage"
	activeWorkSectionBacklog  = "backlog"
	activeWorkSectionQueue    = "queue"
	activeWorkSectionBlockage = "blockage"
	activeWorkSectionFailure  = "failure"
)

// activeWorkSummaryQuery evaluates active_fact_work_items once and derives the
// status snapshot's stage counts, domain backlog, queue snapshot, conflict
// blockages, and latest queue failure from it in a single round trip (#6794).
// Before, each of the five reads embedded activeFactWorkItemsCTE and
// re-evaluated the active set on its own. Each section reuses the standalone
// read's SELECT unchanged and numbers its rows with ROW_NUMBER() over that
// read's own ORDER BY, so row order (including text collation) and the
// blockage/failure limits are decided by Postgres exactly as before. Rows are
// returned as (section, ordinal, to_jsonb(row)). $1 is the snapshot asOf.
var activeWorkSummaryQuery = `
WITH ` + activeFactWorkItemsScopeStateCTE + `,
active_fact_work_items AS MATERIALIZED (
  SELECT ` + activeWorkSummaryColumns + `
  ` + activeFactWorkItemsFromWhere + `
),
` + domainBacklogCTEs + `,
` + reducerConflictBlockageCTEs + `,
active_work_stage AS (
  SELECT ROW_NUMBER() OVER (ORDER BY ` + stageCountsOrder + `) AS ordinal, to_jsonb(section_row) AS section_json
  FROM (
` + stageCountsSelect + `
  ) AS section_row
),
active_work_backlog AS (
  SELECT ROW_NUMBER() OVER (ORDER BY ` + domainBacklogOrder + `) AS ordinal, to_jsonb(section_row) AS section_json
  FROM (
` + domainBacklogSelect + `
  ) AS section_row
),
active_work_queue AS (
  SELECT 1::BIGINT AS ordinal, to_jsonb(section_row) AS section_json
  FROM (
` + queueSnapshotSelect + `
  ) AS section_row
),
active_work_blockage AS (
  SELECT ordinal, section_json
  FROM (
    SELECT ROW_NUMBER() OVER (ORDER BY ` + reducerConflictBlockageOrder + `) AS ordinal, to_jsonb(section_row) AS section_json
    FROM (
` + reducerConflictBlockageSelect + `
    ) AS section_row
  ) AS ranked
  WHERE ordinal <= ` + strconv.Itoa(reducerConflictBlockageLimit) + `
),
active_work_failure AS (
  SELECT ordinal, section_json
  FROM (
    SELECT ROW_NUMBER() OVER (ORDER BY ` + latestQueueFailureOrder + `) AS ordinal, to_jsonb(section_row) AS section_json
    FROM (
` + latestQueueFailureSelect + `
    ) AS section_row
  ) AS ranked
  WHERE ordinal = 1
)
SELECT '` + activeWorkSectionStage + `' AS section, ordinal, section_json::text FROM active_work_stage
UNION ALL
SELECT '` + activeWorkSectionBacklog + `', ordinal, section_json::text FROM active_work_backlog
UNION ALL
SELECT '` + activeWorkSectionQueue + `', ordinal, section_json::text FROM active_work_queue
UNION ALL
SELECT '` + activeWorkSectionBlockage + `', ordinal, section_json::text FROM active_work_blockage
UNION ALL
SELECT '` + activeWorkSectionFailure + `', ordinal, section_json::text FROM active_work_failure
ORDER BY section, ordinal
`

// activeWorkSummary is the part of the raw status snapshot derived from
// active_fact_work_items.
type activeWorkSummary struct {
	StageCounts    []statuspkg.StageStatusCount
	DomainBacklogs []statuspkg.DomainBacklog
	Queue          statuspkg.QueueSnapshot
	Blockages      []statuspkg.QueueBlockage
	LatestFailure  *statuspkg.QueueFailureSnapshot
}

// readActiveWorkSummary runs activeWorkSummaryQuery and decodes each section
// into the same values the five standalone reads produced.
func readActiveWorkSummary(ctx context.Context, queryer db.Queryer, asOf time.Time) (activeWorkSummary, error) {
	rows, err := queryer.QueryContext(ctx, activeWorkSummaryQuery, asOf)
	if err != nil {
		return activeWorkSummary{}, fmt.Errorf("read active work summary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summary := activeWorkSummary{
		StageCounts:    []statuspkg.StageStatusCount{},
		DomainBacklogs: []statuspkg.DomainBacklog{},
		Blockages:      []statuspkg.QueueBlockage{},
	}
	for rows.Next() {
		var section string
		var ordinal int64
		var raw string
		if err := rows.Scan(&section, &ordinal, &raw); err != nil {
			return activeWorkSummary{}, fmt.Errorf("read active work summary: %w", err)
		}
		if err := summary.add(section, raw); err != nil {
			return activeWorkSummary{}, fmt.Errorf("read active work summary %s row %d: %w", section, ordinal, err)
		}
	}
	if err := rows.Err(); err != nil {
		return activeWorkSummary{}, fmt.Errorf("read active work summary: %w", err)
	}
	return summary, nil
}

// add decodes one section row into the summary.
func (s *activeWorkSummary) add(section string, raw string) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var row map[string]any
	if err := decoder.Decode(&row); err != nil {
		return err
	}
	r := &activeWorkRow{fields: row}
	switch section {
	case activeWorkSectionStage:
		s.StageCounts = append(s.StageCounts, statuspkg.StageStatusCount{
			Stage:  r.text("stage"),
			Status: r.text("status"),
			Count:  r.count("count"),
		})
	case activeWorkSectionBacklog:
		s.DomainBacklogs = append(s.DomainBacklogs, statuspkg.DomainBacklog{
			Domain:      r.text("domain"),
			Outstanding: r.count("outstanding_count"),
			InFlight:    r.count("in_flight_count"),
			Retrying:    r.count("retrying_count"),
			DeadLetter:  r.count("dead_letter_count"),
			Failed:      r.count("failed_count"),
			OldestAge:   scalars.DurationFromSeconds(r.seconds("oldest_outstanding_age_seconds")),
		})
	case activeWorkSectionQueue:
		s.Queue = statuspkg.QueueSnapshot{
			Total:                                 r.count("total_count"),
			Outstanding:                           r.count("outstanding_count"),
			Pending:                               r.count("pending_count"),
			InFlight:                              r.count("in_flight_count"),
			Retrying:                              r.count("retrying_count"),
			Succeeded:                             r.count("succeeded_count"),
			DeadLetter:                            r.count("dead_letter_count"),
			Failed:                                r.count("failed_count"),
			ProvenanceEdgeIdentityUpgradeApplied:  r.flag("provenance_edge_identity_upgrade_applied"),
			ProvenanceEdgeIdentityUpgradeRequired: r.count("provenance_edge_identity_upgrade_required"),
			OldestOutstandingAge:                  scalars.DurationFromSeconds(r.seconds("oldest_outstanding_age_seconds")),
			OverdueClaims:                         r.count("overdue_claim_count"),
		}
	case activeWorkSectionBlockage:
		s.Blockages = append(s.Blockages, statuspkg.QueueBlockage{
			Stage:          r.text("stage"),
			Domain:         r.text("domain"),
			ConflictDomain: r.text("conflict_domain"),
			ConflictKey:    r.text("conflict_key"),
			Blocked:        r.count("blocked_count"),
			OldestAge:      scalars.DurationFromSeconds(r.seconds("oldest_blocked_age_seconds")),
		})
	case activeWorkSectionFailure:
		failure := statuspkg.QueueFailureSnapshot{
			Stage:          strings.TrimSpace(r.text("stage")),
			Domain:         strings.TrimSpace(r.text("domain")),
			Status:         strings.TrimSpace(r.text("status")),
			WorkItemID:     strings.TrimSpace(r.text("work_item_id")),
			ScopeID:        strings.TrimSpace(r.text("scope_id")),
			GenerationID:   strings.TrimSpace(r.text("generation_id")),
			FailureClass:   strings.TrimSpace(r.text("failure_class")),
			FailureMessage: strings.TrimSpace(r.text("failure_message")),
			FailureDetails: strings.TrimSpace(r.text("failure_details")),
		}
		updatedAt, err := r.timestamp("updated_at")
		if err != nil {
			return err
		}
		failure.UpdatedAt = updatedAt
		s.LatestFailure = &failure
	default:
		return fmt.Errorf("unknown section %q", section)
	}
	return r.err
}

// activeWorkRow reads typed fields from one decoded section row and keeps the
// first conversion error.
type activeWorkRow struct {
	fields map[string]any
	err    error
}

// lookup returns the value for key, recording an error when the key is absent
// so a renamed or dropped SELECT alias fails loudly (the standalone reads'
// positional Scan did) instead of decoding as a zero value.
func (r *activeWorkRow) lookup(key string) (any, bool) {
	value, ok := r.fields[key]
	if !ok {
		r.fail(fmt.Errorf("missing key %q", key))
	}
	return value, ok
}

// fail records the first decode error on the row.
func (r *activeWorkRow) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *activeWorkRow) text(key string) string {
	value, ok := r.lookup(key)
	if !ok {
		return ""
	}
	text, isText := value.(string)
	if !isText {
		r.fail(fmt.Errorf("%s: want text, got %T", key, value))
	}
	return text
}

func (r *activeWorkRow) flag(key string) bool {
	value, ok := r.lookup(key)
	if !ok {
		return false
	}
	flag, isBool := value.(bool)
	if !isBool {
		r.fail(fmt.Errorf("%s: want boolean, got %T", key, value))
	}
	return flag
}

// count reads a bigint/numeric count. The standalone reads scanned counts into
// int64 and converted to int.
func (r *activeWorkRow) count(key string) int {
	number := r.number(key)
	if number == "" {
		return 0
	}
	value, err := number.Int64()
	if err != nil {
		r.fail(fmt.Errorf("%s: %w", key, err))
	}
	return int(value)
}

// seconds reads an age in seconds. The standalone reads scanned the numeric
// into float64, which pgx converts with strconv.ParseFloat on the decimal
// text; parsing the jsonb number text the same way yields the same float.
func (r *activeWorkRow) seconds(key string) float64 {
	number := r.number(key)
	if number == "" {
		return 0
	}
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil {
		r.fail(fmt.Errorf("%s: %w", key, err))
	}
	return value
}

// number reads a JSON number, recording an error for a missing key or any
// other JSON type.
func (r *activeWorkRow) number(key string) json.Number {
	value, ok := r.lookup(key)
	if !ok {
		return ""
	}
	number, isNumber := value.(json.Number)
	if !isNumber {
		r.fail(fmt.Errorf("%s: want number, got %T", key, value))
	}
	return number
}

// timestamp reads a timestamptz rendered by to_jsonb. JSON null yields the
// zero time, matching the standalone read's sql.NullTime handling; a missing
// key or a non-text value is an error.
func (r *activeWorkRow) timestamp(key string) (time.Time, error) {
	value, ok := r.lookup(key)
	if !ok || value == nil {
		return time.Time{}, nil
	}
	text, isText := value.(string)
	if !isText {
		return time.Time{}, fmt.Errorf("%s: want timestamp text, got %T", key, value)
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", key, err)
	}
	return parsed.UTC(), nil
}
