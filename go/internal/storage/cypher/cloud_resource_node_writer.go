// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseCloudResource names the AWS CloudResource node materialization
// phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseCloudResource = "cloud_resource"

// baseCloudResourceUpsertCypher batches CloudResource node upserts. MERGE
// is on the stable uid identity only; mutable properties are SET separately so
// duplicate input rows and reducer retries converge on one node rather than
// fabricating or duplicating graph state. The shape mirrors the proven
// TerraformResource canonical writer so it engages the same NornicDB
// schema-backed uid lookup and Neo4j planner path.
const baseCloudResourceUpsertCypher = `UNWIND $rows AS row
MERGE (r:CloudResource {uid: row.uid})
SET r.id = row.uid,
    r.arn = row.arn,
    r.resource_id = row.resource_id,
    r.resource_type = row.resource_type,
    r.name = row.name,
    r.state = row.state,
    r.account_id = row.account_id,
    r.region = row.region,
    r.service_kind = row.service_kind,
    r.correlation_anchors = row.correlation_anchors,
    r.service_anchor_status = row.service_anchor_status,
    r.service_anchor_source = row.service_anchor_source,
    r.service_anchor_reason = row.service_anchor_reason,
    r.service_anchor_names = row.service_anchor_names,
    r.service_anchor_name_tokens = row.service_anchor_name_tokens,
    r.workload_id = row.workload_id,
    r.service_name = row.service_name,
    r.running_image_ref = row.running_image_ref,
    r.running_image_digest = row.running_image_digest,
    r.source_fact_id = row.source_fact_id,
    r.stable_fact_key = row.stable_fact_key,
    r.source_system = row.source_system,
    r.source_record_id = row.source_record_id,
    r.source_confidence = row.source_confidence,
    r.collector_kind = row.collector_kind,
    r.evidence_source = row.evidence_source`

// canonicalCloudResourceUpsertCypher is the statement WriteCloudResourceNodes
// actually executes: baseCloudResourceUpsertCypher plus
// teethCloudResourceUpsertExtraSet, which is the empty string in every
// normal build (cloud_resource_node_writer_teeth_off.go) and exactly one
// extra SET clause under the ifadeterminismteeth build tag
// (cloud_resource_node_writer_teeth.go) — see that file's doc for why. Both
// operands are untyped string constants, so this concatenation is itself a
// compile-time constant; no normal build pays a runtime cost for the split.
const canonicalCloudResourceUpsertCypher = baseCloudResourceUpsertCypher + teethCloudResourceUpsertExtraSet

// cloudResourceRowKeyDefault pairs one row.<key> reference
// baseCloudResourceUpsertCypher's SET clause reads with its typed zero value.
type cloudResourceRowKeyDefault struct {
	key   string
	value any
}

// cloudResourceRowKeyDefaults is the single authoritative list of every
// row.<key> reference baseCloudResourceUpsertCypher's SET clause reads
// (excluding "uid", the MERGE identity every caller must already supply, and
// "evidence_source", which WriteCloudResourceNodes itself always injects
// below). WriteCloudResourceNodes uses it to default-fill any key a caller's
// row map omits — issue #5714/#5055's shared-writer backstop (Option A,
// chosen over an in-Cypher `coalesce` rewrite after both were measured live
// against the pinned NornicDB image: see
// docs/internal/evidence/5714-cloudresource-row-key-defaults.md).
//
// Every present-and-set CloudResource row builder (AWS, Azure, GCP) already
// supplies all of these keys explicitly (the #4995/#5450/#5714 precedent), so
// this defaulting is normally a no-op; it exists so a FUTURE row builder that
// forgets one cannot reproduce the class of bug this issue fixed. The pinned
// NornicDB backend does not evaluate a key MISSING from one row of a
// heterogeneous UNWIND $rows batch as null in a SET clause — it persists a
// stringified representation of the row expression instead (e.g.
// "row.workload_id", a non-empty string a query/join consumer would treat as
// real data).
//
// A fixed-order slice, not a map: WriteCloudResourceNodes runs this over
// every row in a batch (up to DefaultBatchSize), and ranging a Go map costs
// measurably more per entry than a slice (hashing plus randomized iteration
// order) for no benefit here — see the Performance Evidence note in
// go/internal/storage/cypher/README.md. Keep this slice in lockstep with
// baseCloudResourceUpsertCypher's SET clause:
// TestCloudResourceRowKeyDefaultsCoversEverySetKey fails the build if a SET
// key has no matching default (or vice versa).
var cloudResourceRowKeyDefaults = []cloudResourceRowKeyDefault{
	{"arn", ""},
	{"resource_id", ""},
	{"resource_type", ""},
	{"name", ""},
	{"state", ""},
	{"account_id", ""},
	{"region", ""},
	{"service_kind", ""},
	{"correlation_anchors", []string{}},
	{"service_anchor_status", ""},
	{"service_anchor_source", ""},
	{"service_anchor_reason", ""},
	{"service_anchor_names", []string{}},
	{"service_anchor_name_tokens", ""},
	{"workload_id", ""},
	{"service_name", ""},
	{"running_image_ref", ""},
	{"running_image_digest", ""},
	{"source_fact_id", ""},
	{"stable_fact_key", ""},
	{"source_system", ""},
	{"source_record_id", ""},
	{"source_confidence", ""},
	{"collector_kind", ""},
}

// defaultFillCloudResourceRow ensures every key cloudResourceRowKeyDefaults
// names is present on row, filling any missing key with its typed zero value.
// A key row already carries (even if empty/zero-valued) is left untouched —
// this only fills OMITTED keys, never overwrites a caller-supplied value.
func defaultFillCloudResourceRow(row map[string]any) {
	for _, d := range cloudResourceRowKeyDefaults {
		if _, present := row[d.key]; !present {
			row[d.key] = d.value
		}
	}
}

// CloudResourceNodeWriter materializes aws_resource facts into canonical
// CloudResource graph nodes. It satisfies the reducer-owned
// CloudResourceNodeWriter consumer interface and writes through the
// backend-neutral Executor seam.
type CloudResourceNodeWriter struct {
	executor  Executor
	batchSize int
}

// NewCloudResourceNodeWriter returns a CloudResourceNodeWriter backed by the
// given Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewCloudResourceNodeWriter(executor Executor, batchSize int) *CloudResourceNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CloudResourceNodeWriter{executor: executor, batchSize: batchSize}
}

// WriteCloudResourceNodes upserts CloudResource nodes for the given rows using
// batched UNWIND statements. When the executor implements GroupExecutor all
// batches are dispatched in a single atomic transaction; otherwise they run
// sequentially. The write is idempotent: the same uid converges on one node
// across batches, retries, and generations.
func (w *CloudResourceNodeWriter) WriteCloudResourceNodes(
	ctx context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("cloud resource node writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned := make(map[string]any, len(row)+1)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["evidence_source"] = evidenceSource
		defaultFillCloudResourceRow(cloned)
		annotated = append(annotated, cloned)
	}

	stmts := buildBatchedStatements(canonicalCloudResourceUpsertCypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseCloudResource
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = "CloudResource"
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=CloudResource rows=%d",
			len(batchRows),
		)
	}

	if ge, ok := w.executor.(GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		return nil
	}

	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
