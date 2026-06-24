// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseCidrBlock and canonicalPhasePrefixList name the security-group
// endpoint node materialization phases for grouped-backend statement metadata
// and diagnostics (issue #1135 PR2a).
const (
	canonicalPhaseCidrBlock  = "cidr_block"
	canonicalPhasePrefixList = "prefix_list"
)

// canonicalCidrBlockUpsertCypher batches CidrBlock node upserts. MERGE is on the
// stable uid identity only (a deterministic hash of the canonicalized CIDR and
// address family); mutable properties are SET separately so duplicate input rows
// and reducer retries converge on one node rather than fabricating or
// duplicating graph state. The shape mirrors the proven CloudResource canonical
// writer so it engages the same NornicDB schema-backed uid lookup and Neo4j
// planner path.
//
// The cidr property carries the canonical masked, lowercased form (host bits
// zeroed, IPv6 zero-compressed) so two security-group rules naming the same
// network with different host bits or casing converge on one endpoint node.
// is_internet is true only for the canonical open CIDRs 0.0.0.0/0 and ::/0.
const canonicalCidrBlockUpsertCypher = `UNWIND $rows AS row
MERGE (c:CidrBlock {uid: row.uid})
SET c.id = row.uid,
    c.cidr = row.cidr,
    c.address_family = row.address_family,
    c.is_internet = row.is_internet,
    c.source_fact_id = row.source_fact_id,
    c.stable_fact_key = row.stable_fact_key,
    c.source_system = row.source_system,
    c.source_record_id = row.source_record_id,
    c.collector_kind = row.collector_kind,
    c.evidence_source = row.evidence_source`

// canonicalPrefixListUpsertCypher batches PrefixList node upserts. MERGE is on
// the stable uid identity only (a deterministic hash of the prefix-list id plus
// the account and region that scope it); mutable properties are SET separately
// so duplicate input rows and reducer retries converge on one node. Prefix-list
// ids are unique only within an account/region visibility scope, so the uid
// folds account_id and region into the identity rather than keying on the bare
// pl-id.
const canonicalPrefixListUpsertCypher = `UNWIND $rows AS row
MERGE (p:PrefixList {uid: row.uid})
SET p.id = row.uid,
    p.prefix_list_id = row.prefix_list_id,
    p.account_id = row.account_id,
    p.region = row.region,
    p.source_fact_id = row.source_fact_id,
    p.stable_fact_key = row.stable_fact_key,
    p.source_system = row.source_system,
    p.source_record_id = row.source_record_id,
    p.collector_kind = row.collector_kind,
    p.evidence_source = row.evidence_source`

// CidrBlockNodeWriter materializes the CIDR endpoints of aws_security_group_rule
// facts into canonical CidrBlock graph nodes. It satisfies the reducer-owned
// node-writer consumer interface and writes through the backend-neutral Executor
// seam (issue #1135 PR2a).
type CidrBlockNodeWriter struct {
	executor  Executor
	batchSize int
}

// NewCidrBlockNodeWriter returns a CidrBlockNodeWriter backed by the given
// Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewCidrBlockNodeWriter(executor Executor, batchSize int) *CidrBlockNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CidrBlockNodeWriter{executor: executor, batchSize: batchSize}
}

// WriteCidrBlockNodes upserts CidrBlock nodes for the given rows using batched
// UNWIND statements. When the executor implements GroupExecutor all batches are
// dispatched in a single atomic transaction; otherwise they run sequentially.
// The write is idempotent: the same uid converges on one node across batches,
// retries, and generations.
func (w *CidrBlockNodeWriter) WriteCidrBlockNodes(
	ctx context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	return writeSecurityGroupEndpointNodes(
		ctx,
		w.executor,
		"cidr block",
		canonicalCidrBlockUpsertCypher,
		canonicalPhaseCidrBlock,
		"CidrBlock",
		rows,
		evidenceSource,
		w.batchSize,
	)
}

// PrefixListNodeWriter materializes the managed-prefix-list endpoints of
// aws_security_group_rule facts into canonical PrefixList graph nodes. It
// satisfies the reducer-owned node-writer consumer interface and writes through
// the backend-neutral Executor seam (issue #1135 PR2a).
type PrefixListNodeWriter struct {
	executor  Executor
	batchSize int
}

// NewPrefixListNodeWriter returns a PrefixListNodeWriter backed by the given
// Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewPrefixListNodeWriter(executor Executor, batchSize int) *PrefixListNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &PrefixListNodeWriter{executor: executor, batchSize: batchSize}
}

// WritePrefixListNodes upserts PrefixList nodes for the given rows using batched
// UNWIND statements. When the executor implements GroupExecutor all batches are
// dispatched in a single atomic transaction; otherwise they run sequentially.
// The write is idempotent: the same uid converges on one node across batches,
// retries, and generations.
func (w *PrefixListNodeWriter) WritePrefixListNodes(
	ctx context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	return writeSecurityGroupEndpointNodes(
		ctx,
		w.executor,
		"prefix list",
		canonicalPrefixListUpsertCypher,
		canonicalPhasePrefixList,
		"PrefixList",
		rows,
		evidenceSource,
		w.batchSize,
	)
}

// SecurityGroupEndpointNodeWriter is the composite node writer the reducer's
// security-group endpoint materialization domain consumes. It pairs the CidrBlock
// and PrefixList writers so one value satisfies the reducer-owned interface that
// requires both WriteCidrBlockNodes and WritePrefixListNodes.
type SecurityGroupEndpointNodeWriter struct {
	*CidrBlockNodeWriter
	*PrefixListNodeWriter
}

// NewSecurityGroupEndpointNodeWriter returns a composite writer backed by the
// given Executor, with both endpoint writers sharing the batch size.
func NewSecurityGroupEndpointNodeWriter(executor Executor, batchSize int) *SecurityGroupEndpointNodeWriter {
	return &SecurityGroupEndpointNodeWriter{
		CidrBlockNodeWriter:  NewCidrBlockNodeWriter(executor, batchSize),
		PrefixListNodeWriter: NewPrefixListNodeWriter(executor, batchSize),
	}
}

// writeSecurityGroupEndpointNodes is the shared batched canonical-upsert path for
// the CidrBlock and PrefixList writers. Both labels share the identical
// idempotent-uid MERGE shape, batch metadata, and atomic group dispatch, so the
// only differences are the statement, phase tag, and label name. Keeping one
// implementation avoids drift between the two endpoint writers.
func writeSecurityGroupEndpointNodes(
	ctx context.Context,
	executor Executor,
	writerName string,
	upsertCypher string,
	phase string,
	label string,
	rows []map[string]any,
	evidenceSource string,
	batchSize int,
) error {
	if len(rows) == 0 {
		return nil
	}
	if executor == nil {
		return fmt.Errorf("%s node writer executor is required", writerName)
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned := make(map[string]any, len(row)+1)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["evidence_source"] = evidenceSource
		annotated = append(annotated, cloned)
	}

	stmts := buildBatchedStatements(upsertCypher, annotated, batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = phase
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = label
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d",
			label,
			len(batchRows),
		)
	}

	if ge, ok := executor.(GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		return nil
	}

	for _, stmt := range stmts {
		if err := executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
