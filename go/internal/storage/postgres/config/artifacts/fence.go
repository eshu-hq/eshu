// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fenceEventName is the event_name of every log record the fence check
// writes, in the style of postgres.server_encoding.
const fenceEventName = "postgres.config_artifact_fence"

// fenceTriggers is the one list of version-fence triggers the start-up check
// requires: the four statement-level triggers of migration 186 on
// content_files and its deferred constraint trigger on the pending table.
// TestFenceTriggersMatchMigration186 fails when the embedded migration's
// CREATE TRIGGER statements diverge from it.
var fenceTriggers = []FenceTrigger{
	{Table: "content_files", Name: "content_files_version_fence_insert"},
	{Table: "content_files", Name: "content_files_version_fence_update"},
	{Table: "content_files", Name: "content_files_version_fence_delete"},
	{Table: "content_files", Name: "content_files_version_fence_truncate"},
	{Table: "config_artifact_fence_pending", Name: "config_artifact_fence_pending_commit"},
}

// fenceTriggersSQL returns, in one round trip, the tgenabled of each
// requested (table, trigger) pair that exists as a non-internal trigger on
// that table. to_regclass resolves the table through search_path, as the
// migrations' unqualified names do, and yields NULL for a missing table, so a
// database without the schema returns no rows instead of an error. A pair
// with no row is absent.
const fenceTriggersSQL = `
SELECT want.table_name, want.trigger_name, tg.tgenabled::text
FROM unnest($1::text[], $2::text[]) AS want(table_name, trigger_name)
JOIN pg_trigger tg
  ON tg.tgrelid = to_regclass(want.table_name)
 AND tg.tgname = want.trigger_name
 AND NOT tg.tgisinternal`

// FenceTrigger names one version-fence trigger and the table it must be
// defined on. A trigger of the same name on another table does not count.
type FenceTrigger struct {
	Table string
	Name  string
}

// MissingFenceTrigger is a required fence trigger that does not fire for an
// ordinary session. Enabled is its pg_trigger.tgenabled: empty when the
// trigger does not exist on its table, "D" when disabled, "R" when it fires
// only in a session whose replication role is replica.
type MissingFenceTrigger struct {
	FenceTrigger
	Enabled string
}

// Fence is the result of the start-up fence check (S3 ruling, arb-8155-f1).
// When Supported is false the caller skips every derive and reports the read
// model as StatusNotInstalled with Reason; content Writes and live reads
// proceed unchanged. A Deriver given an unsupported Fence returns
// OutcomeFenceMissing for a missing or non-firing trigger and for the
// failed-catalog path (ReasonFenceCheckFailed) alike, as a failed encoding
// read maps to OutcomeEncodingUnsupported. Missing lists the absent or
// non-firing triggers in fenceTriggers order; it is empty when the catalog
// could not be read.
type Fence struct {
	Supported bool
	Reason    string
	Missing   []MissingFenceTrigger
}

// CheckFence reads pg_trigger once and requires each of the five migration
// 186 fence triggers to exist on its table with tgenabled 'O' (origin) or 'A'
// (always). A disabled ('D') or replica-only ('R') trigger leaves content
// writes unfenced, so it counts as missing. It logs under the event
// postgres.config_artifact_fence: at INFO when all five fire, at ERROR naming
// each missing trigger and its tgenabled otherwise. It never fails the
// caller. A failed catalog read is reported as unsupported with
// ReasonFenceCheckFailed, so the read model stays off rather than trusting
// rows behind a fence the process cannot vouch for. A nil logger logs
// nothing.
func CheckFence(ctx context.Context, queryer db.Queryer, logger *slog.Logger) Fence {
	enabled, err := readFenceTriggers(ctx, queryer)
	if err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "config artifact version fence check failed; config artifact read model disabled",
				"event_name", fenceEventName,
				"config_artifacts_status", StatusNotInstalled,
				"reason", ReasonFenceCheckFailed,
				"error", err.Error())
		}
		return Fence{Reason: ReasonFenceCheckFailed}
	}
	var missing []MissingFenceTrigger
	for _, trigger := range fenceTriggers {
		state := enabled[trigger]
		if state == "O" || state == "A" {
			continue
		}
		missing = append(missing, MissingFenceTrigger{FenceTrigger: trigger, Enabled: state})
	}
	if len(missing) > 0 {
		if logger != nil {
			logger.ErrorContext(ctx, "config artifact version fence trigger missing or disabled; config artifact read model disabled",
				"event_name", fenceEventName,
				"missing_triggers", missingTriggerLabels(missing),
				"missing_count", len(missing),
				"config_artifacts_status", StatusNotInstalled,
				"reason", ReasonVersionFenceMissing,
				"outcome", string(OutcomeFenceMissing))
		}
		return Fence{Reason: ReasonVersionFenceMissing, Missing: missing}
	}
	if logger != nil {
		logger.InfoContext(ctx, "config artifact version fence triggers present",
			"event_name", fenceEventName,
			"fence_triggers", len(fenceTriggers))
	}
	return Fence{Supported: true}
}

// missingTriggerLabels renders each missing trigger as table.name=state,
// where state is its tgenabled or "absent".
func missingTriggerLabels(missing []MissingFenceTrigger) []string {
	labels := make([]string, 0, len(missing))
	for _, trigger := range missing {
		state := trigger.Enabled
		if state == "" {
			state = "absent"
		}
		labels = append(labels, trigger.Table+"."+trigger.Name+"="+state)
	}
	return labels
}

// readFenceTriggers returns the tgenabled of each required trigger that
// exists on its table.
func readFenceTriggers(ctx context.Context, queryer db.Queryer) (map[FenceTrigger]string, error) {
	tables := make([]string, 0, len(fenceTriggers))
	names := make([]string, 0, len(fenceTriggers))
	for _, trigger := range fenceTriggers {
		tables = append(tables, trigger.Table)
		names = append(names, trigger.Name)
	}
	rows, err := queryer.QueryContext(ctx, fenceTriggersSQL, tables, names)
	if err != nil {
		return nil, fmt.Errorf("read config artifact fence triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	enabled := make(map[FenceTrigger]string, len(fenceTriggers))
	for rows.Next() {
		var trigger FenceTrigger
		var state string
		if err := rows.Scan(&trigger.Table, &trigger.Name, &state); err != nil {
			return nil, fmt.Errorf("scan config artifact fence trigger: %w", err)
		}
		enabled[trigger] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read config artifact fence triggers: %w", err)
	}
	return enabled, nil
}
