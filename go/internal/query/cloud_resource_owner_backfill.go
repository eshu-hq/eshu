// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	cloudResourceBackfillDefaultPageSize = 500
	// Backfilled rows describe graph state written before the owner ledger was
	// available. A minimum order key preserves that state without letting a
	// migration race overwrite any real reducer contribution, whose observed-at
	// timestamp is always greater than this sentinel.
	cloudResourceBackfillMinimumOrderKeyPrefix = postgres.GraphNodeOwnerBackfillMinimumOrderKeyPrefix
)

const cloudResourceOwnerBackfillProjection = `
       n.uid AS uid,
       n.id AS id,
       n.arn AS arn,
       n.resource_id AS resource_id,
       n.resource_type AS resource_type,
       n.name AS name,
       n.state AS state,
       n.account_id AS account_id,
       n.region AS region,
       n.service_kind AS service_kind,
       n.correlation_anchors AS correlation_anchors,
       n.service_anchor_status AS service_anchor_status,
       n.service_anchor_source AS service_anchor_source,
       n.service_anchor_reason AS service_anchor_reason,
       n.service_anchor_names AS service_anchor_names,
       n.service_anchor_name_tokens AS service_anchor_name_tokens,
       n.workload_id AS workload_id,
       n.service_name AS service_name,
       n.imds_v2_required AS imds_v2_required,
       n.imds_http_endpoint AS imds_http_endpoint,
       n.imds_http_put_hop_limit AS imds_http_put_hop_limit,
       n.user_data_present AS user_data_present,
       n.detailed_monitoring_enabled AS detailed_monitoring_enabled,
       n.ebs_optimized AS ebs_optimized,
       n.public_ip_associated AS public_ip_associated,
       n.instance_profile_arn AS instance_profile_arn,
       n.tenancy AS tenancy,
       n.nitro_enclave_enabled AS nitro_enclave_enabled,
       n.source_fact_id AS source_fact_id,
       n.stable_fact_key AS stable_fact_key,
       n.source_system AS source_system,
       n.source_record_id AS source_record_id,
       n.source_confidence AS source_confidence,
       n.collector_kind AS collector_kind,
       n.evidence_source AS evidence_source`

const cloudResourceOwnerBackfillFirstPageQuery = `
MATCH (n:CloudResource)
RETURN ` + cloudResourceOwnerBackfillProjection + `
ORDER BY n.uid
LIMIT $limit`

const cloudResourceOwnerBackfillNextPageQuery = `
MATCH (n:CloudResource)
WHERE n.uid > $after_uid
RETURN ` + cloudResourceOwnerBackfillProjection + `
ORDER BY n.uid
LIMIT $limit`

// CloudResourceOwnerBackfillStore is the durable migration seam used to seed
// graph_node_owner before the indexed cloud-resource read path is exposed.
type CloudResourceOwnerBackfillStore interface {
	IsCloudResourceBackfillComplete(context.Context) (bool, error)
	SeedExistingGraphNodeOwners(context.Context, []postgres.GraphNodeOwnerEntry, time.Time) error
	MarkCloudResourceBackfillComplete(context.Context, time.Time) error
}

// CloudResourceOwnerBackfiller copies existing CloudResource graph rows into
// graph_node_owner once. It preserves the graph row as-is and assigns a minimum
// order key so concurrent or later reducer writes always win through the normal
// max-order-key gate.
type CloudResourceOwnerBackfiller struct {
	Graph    GraphQuery
	Store    CloudResourceOwnerBackfillStore
	PageSize int
	Now      func() time.Time
}

// BackfillCloudResourceOwnerLedger runs the production upgrade backfill before
// API or MCP routes can select a page from graph_node_owner.
func BackfillCloudResourceOwnerLedger(ctx context.Context, db *sql.DB, graph GraphQuery) error {
	if db == nil {
		return fmt.Errorf("cloud resource owner backfill database is required")
	}
	store := postgres.NewGraphNodeOwnerBackfillStore(postgres.SQLDB{DB: db})
	return (CloudResourceOwnerBackfiller{Graph: graph, Store: store}).Run(ctx)
}

// Run seeds every existing CloudResource row before recording durable
// completion. A partial failure never marks completion, so the next startup
// retries the idempotent, monotonic seed.
func (b CloudResourceOwnerBackfiller) Run(ctx context.Context) error {
	if b.Graph == nil {
		return fmt.Errorf("cloud resource owner backfill graph is required")
	}
	if b.Store == nil {
		return fmt.Errorf("cloud resource owner backfill store is required")
	}
	complete, err := b.Store.IsCloudResourceBackfillComplete(ctx)
	if err != nil {
		return fmt.Errorf("check cloud resource owner backfill completion: %w", err)
	}
	if complete {
		return nil
	}

	pageSize := b.PageSize
	if pageSize <= 0 {
		pageSize = cloudResourceBackfillDefaultPageSize
	}
	now := time.Now().UTC()
	if b.Now != nil {
		now = b.Now().UTC()
	}

	afterUID := ""
	started := time.Now()
	pagesSeeded := 0
	rowsSeeded := 0
	for {
		query := cloudResourceOwnerBackfillFirstPageQuery
		if afterUID != "" {
			query = cloudResourceOwnerBackfillNextPageQuery
		}
		rows, err := b.Graph.Run(ctx, query, map[string]any{
			"after_uid": afterUID,
			"limit":     pageSize,
		})
		if err != nil {
			return fmt.Errorf("enumerate existing cloud resources after %q: %w", afterUID, err)
		}
		if len(rows) == 0 {
			break
		}

		entries, err := cloudResourceBackfillEntries(rows)
		if err != nil {
			return err
		}
		if err := b.Store.SeedExistingGraphNodeOwners(ctx, entries, now); err != nil {
			return fmt.Errorf("seed existing cloud resource owners: %w", err)
		}
		pagesSeeded++
		rowsSeeded += len(entries)
		afterUID = entries[len(entries)-1].UID
		if len(rows) < pageSize {
			break
		}
	}

	if err := b.Store.MarkCloudResourceBackfillComplete(ctx, now); err != nil {
		return fmt.Errorf("mark cloud resource owner backfill complete: %w", err)
	}
	slog.InfoContext(
		ctx,
		"cloud resource owner ledger backfill complete",
		"pages_seeded", pagesSeeded,
		"rows_seeded", rowsSeeded,
		"duration_seconds", time.Since(started).Seconds(),
	)
	return nil
}

func cloudResourceBackfillEntries(rows []map[string]any) ([]postgres.GraphNodeOwnerEntry, error) {
	entries := make([]postgres.GraphNodeOwnerEntry, 0, len(rows))
	previousUID := ""
	for _, graphRow := range rows {
		uid := strings.TrimSpace(StringVal(graphRow, "uid"))
		factID := strings.TrimSpace(StringVal(graphRow, "source_fact_id"))
		resourceType := strings.TrimSpace(StringVal(graphRow, "resource_type"))
		if uid == "" || factID == "" || resourceType == "" {
			return nil, fmt.Errorf(
				"cloud resource owner backfill row requires uid, resource_type, and source_fact_id: uid=%q resource_type=%q source_fact_id=%q",
				uid, resourceType, factID,
			)
		}
		if previousUID != "" && uid <= previousUID {
			return nil, fmt.Errorf("cloud resource owner backfill rows are not strictly ordered: %q after %q", uid, previousUID)
		}
		winningRow := clonePresentCloudResourceBackfillFields(graphRow)
		winningRow["uid"] = uid
		winningRow["source_order_key"] = cloudResourceBackfillMinimumOrderKeyPrefix + factID
		encoded, err := json.Marshal(winningRow)
		if err != nil {
			return nil, fmt.Errorf("encode cloud resource owner backfill row %q: %w", uid, err)
		}
		entries = append(entries, postgres.GraphNodeOwnerEntry{
			UID:            uid,
			SourceOrderKey: cloudResourceBackfillMinimumOrderKeyPrefix + factID,
			WinningRow:     encoded,
		})
		previousUID = uid
	}
	return entries, nil
}

func clonePresentCloudResourceBackfillFields(row map[string]any) map[string]any {
	cloned := make(map[string]any, len(row)+1)
	for key, value := range row {
		if value != nil {
			cloned[key] = value
		}
	}
	return cloned
}
