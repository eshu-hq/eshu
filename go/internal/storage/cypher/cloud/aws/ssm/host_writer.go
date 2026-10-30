// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssm

import (
	"context"
	"fmt"
	"strings"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// HostTuple is the EC2 managed-node observation boundary: the AWS partition,
// account, region, and managed node instance identifier.
type HostTuple struct {
	Partition  string
	AccountID  string
	Region     string
	InstanceID string
}

// HostKey encodes one host tuple for grouping. The NUL separator cannot
// appear in a validated tuple field, so distinct tuples never collide. It
// mirrors ssmhost.HostKey field for field; the cmd/reducer adapter maps
// between the two so this package never imports the reducer.
func HostKey(host HostTuple) string {
	return strings.Join([]string{host.Partition, host.AccountID, host.Region, host.InstanceID}, "\x00")
}

// CommittedHost is the projection the graph already holds for one host: the
// newest committed application capture and, per platform provenance, the
// committed evidence time. The reducer reads it back so a delayed or
// duplicated older capture can never supersede (and retract) newer truth.
type CommittedHost struct {
	AppCapture       time.Time
	HasAppCapture    bool
	PlatformCaptures map[string]time.Time
}

const canonicalPhaseSSMHost = "ssm_host_inventory"

const (
	ssmHostApplicationLabel = "SSMHostApplication"
	ssmHostPlatformLabel    = "SSMHostPlatform"
)

// reportedWithRelationship is the closed single-member relationship
// vocabulary this writer may emit. The type is interpolated into the
// relationship-type position (which cannot be parameterized), so a row
// carrying any other value is rejected rather than turned into an arbitrary
// relationship type. It mirrors edgetype.ReportedWith; the duplication is
// intentional because the cypher writer owns the relationship-type position
// and must not depend on reducer internals.
const reportedWithRelationship = "REPORTED_WITH"

// canonicalSSMHostApplicationUpsertCypher MERGEs observation nodes by stable
// uid (never CREATE: the MERGE key is the true identity, mutable properties
// ride SET). MERGE-anchoring follows the #5652 fix: a bare-MATCH-anchored
// UNWIND SET silently drops its write on the pinned NornicDB image.
const canonicalSSMHostApplicationUpsertCypher = `UNWIND $rows AS row
MERGE (n:SSMHostApplication {uid: row.uid})
SET n.partition = row.partition,
    n.account_id = row.account_id,
    n.region = row.region,
    n.instance_id = row.instance_id,
    n.resource_type = row.resource_type,
    n.source_type = row.source_type,
    n.schema_version = row.schema_version,
    n.capture_time = row.capture_time,
    n.capture_time_unix = row.capture_time_unix,
    n.name = row.name,
    n.version = row.version,
    n.release = row.release,
    n.epoch = row.epoch,
    n.package_id = row.package_id,
    n.architecture = row.architecture,
    n.scope_id = row.scope_id,
    n.generation_id = row.generation_id,
    n.evidence_source = row.evidence_source,
    n.source_fact_id = row.source_fact_id,
    n.source_order_key = row.source_order_key`

const canonicalSSMHostPlatformUpsertCypher = `UNWIND $rows AS row
MERGE (n:SSMHostPlatform {uid: row.uid})
SET n.partition = row.partition,
    n.account_id = row.account_id,
    n.region = row.region,
    n.instance_id = row.instance_id,
    n.resource_type = row.resource_type,
    n.source_type = row.source_type,
    n.platform_type = row.platform_type,
    n.platform_name = row.platform_name,
    n.platform_version = row.platform_version,
    n.capture_time = row.capture_time,
    n.observation_time = row.observation_time,
    n.observation_time_unix = row.observation_time_unix,
    n.scope_id = row.scope_id,
    n.generation_id = row.generation_id,
    n.evidence_source = row.evidence_source,
    n.source_fact_id = row.source_fact_id,
    n.source_order_key = row.source_order_key`

// canonicalSSMHostReportedWithEdgeUpsertCypherFormat batches REPORTED_WITH
// pairing edges between one generation's application and platform observation
// nodes. Two anchored MATCHes precede the MERGE so a row whose endpoint is
// absent produces no edge and no fabricated node, and so the two independent
// MATCHes never form a cartesian product. The relationship type is the
// validated static token (the single %s).
const canonicalSSMHostReportedWithEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (source:SSMHostApplication {uid: row.from_uid})
MATCH (target:SSMHostPlatform {uid: row.to_uid})
MERGE (source)-[rel:%s]->(target)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// HostNodeWriter writes reducer-owned SSM host inventory observation nodes,
// platform-pairing edges, and supersession retracts through the
// backend-neutral Executor seam. Node and edge writes are uid-idempotent
// MERGEs; retracts are host- and evidence-scoped DETACH DELETEs that never
// touch another host's preserved truth.
type HostNodeWriter struct {
	executor  sourcecypher.Executor
	reader    sourcecypher.PostureExistenceReader
	batchSize int
}

// NewHostNodeWriter returns a HostNodeWriter backed by the given Executor. A
// nil reader disables the committed-capture read (the reducer then treats
// every complete capture as superseding). A batchSize of 0 or less uses
// DefaultBatchSize.
func NewHostNodeWriter(executor sourcecypher.Executor, reader sourcecypher.PostureExistenceReader, batchSize int) *HostNodeWriter {
	if batchSize <= 0 {
		batchSize = sourcecypher.DefaultBatchSize
	}
	return &HostNodeWriter{executor: executor, reader: reader, batchSize: batchSize}
}

// WriteApplicationNodes merges application observation nodes by stable uid,
// annotating every row with scope, generation, evidence source, and the unix
// capture time the supersession retract compares.
func (w *HostNodeWriter) WriteApplicationNodes(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ssm host node writer executor is required")
	}
	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		captureUnix, err := captureUnix(row)
		if err != nil {
			return err
		}
		annotated = append(annotated, sourcecypher.CloneRowWith(row, map[string]any{
			"scope_id":          scopeID,
			"generation_id":     generationID,
			"evidence_source":   evidenceSource,
			"capture_time_unix": captureUnix,
		}))
	}
	return w.dispatch(ctx, canonicalSSMHostApplicationUpsertCypher, ssmHostApplicationLabel, annotated)
}

// WritePlatformNodes merges platform observation nodes by stable uid,
// annotating every row with scope, generation, evidence source, and the unix
// evidence time.
func (w *HostNodeWriter) WritePlatformNodes(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ssm host node writer executor is required")
	}
	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		evidenceUnix, err := platformEvidenceUnix(row)
		if err != nil {
			return err
		}
		annotated = append(annotated, sourcecypher.CloneRowWith(row, map[string]any{
			"scope_id":              scopeID,
			"generation_id":         generationID,
			"evidence_source":       evidenceSource,
			"observation_time_unix": evidenceUnix,
		}))
	}
	return w.dispatch(ctx, canonicalSSMHostPlatformUpsertCypher, ssmHostPlatformLabel, annotated)
}

// WriteReportedWithEdges merges pairing edges between committed observation
// nodes. Every row's relationship screens against the closed vocabulary
// before the static token interpolates, so a deviating row can never
// fabricate a new relationship type.
func (w *HostNodeWriter) WriteReportedWithEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ssm host edge writer executor is required")
	}
	for _, row := range rows {
		relationship, _ := row["relationship"].(string)
		if relationship != reportedWithRelationship {
			return fmt.Errorf("ssm host edge writer: relationship %q is outside the closed REPORTED_WITH vocabulary", relationship)
		}
	}
	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		annotated = append(annotated, sourcecypher.CloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}
	cypher := fmt.Sprintf(canonicalSSMHostReportedWithEdgeUpsertCypherFormat, reportedWithRelationship)
	return w.dispatch(ctx, cypher, "SSMHostApplication_REPORTED_WITH_SSMHostPlatform", annotated)
}

// WriteHostObservationBatch writes one host's nodes and pairing edges with
// node-then-edge dispatch discipline: edges commit only after their endpoint
// nodes commit, so an edge MATCH never runs against nodes another statement
// in the same transaction has yet to make visible (the package_registry
// deferral precedent for NornicDB read-your-writes).
func (w *HostNodeWriter) WriteHostObservationBatch(
	ctx context.Context,
	apps []map[string]any,
	platforms []map[string]any,
	edges []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if err := w.WriteApplicationNodes(ctx, apps, scopeID, generationID, evidenceSource); err != nil {
		return err
	}
	if err := w.WritePlatformNodes(ctx, platforms, scopeID, generationID, evidenceSource); err != nil {
		return err
	}
	return w.WriteReportedWithEdges(ctx, edges, scopeID, generationID, evidenceSource)
}

// ReadCommittedHosts returns the newest committed evidence times the graph
// holds for each host in one batched read. A nil reader reports no committed
// state so every complete capture supersedes (the unwired path matches the
// first-write behavior exactly).
func captureUnix(row map[string]any) (int64, error) {
	raw, _ := row["capture_time"].(string)
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0, fmt.Errorf("ssm host node writer: application row capture_time %q does not parse RFC3339: %w", raw, err)
	}
	return parsed.UTC().Unix(), nil
}

func platformEvidenceUnix(row map[string]any) (int64, error) {
	if raw, _ := row["capture_time"].(string); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return 0, fmt.Errorf("ssm host node writer: platform row capture_time %q does not parse RFC3339: %w", raw, err)
		}
		return parsed.UTC().Unix(), nil
	}
	raw, _ := row["observation_time"].(string)
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0, fmt.Errorf("ssm host node writer: platform row observation_time %q does not parse RFC3339: %w", raw, err)
	}
	return parsed.UTC().Unix(), nil
}

func stringVal(value any) string {
	text, _ := value.(string)
	return text
}

func intVal(value any) (int64, bool) {
	switch num := value.(type) {
	case int64:
		return num, true
	case int:
		return int64(num), true
	case float64:
		return int64(num), true
	}
	return 0, false
}

func (w *HostNodeWriter) dispatch(ctx context.Context, cypher, label string, rows []map[string]any) error {
	stmts := sourcecypher.BuildBatchedStatements(cypher, rows, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[sourcecypher.StatementMetadataPhaseKey] = canonicalPhaseSSMHost
		stmts[index].Parameters[sourcecypher.StatementMetadataEntityLabelKey] = label
		stmts[index].Parameters[sourcecypher.StatementMetadataSummaryKey] = fmt.Sprintf(
			"node_properties=%s rows=%d",
			label,
			len(batchRows),
		)
	}
	if ge, ok := w.executor.(sourcecypher.GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(err)
		}
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

func withStatementMetadata(params map[string]any, summary string) map[string]any {
	params[sourcecypher.StatementMetadataPhaseKey] = canonicalPhaseSSMHost
	params[sourcecypher.StatementMetadataEntityLabelKey] = ssmHostApplicationLabel
	params[sourcecypher.StatementMetadataSummaryKey] = summary
	return params
}
