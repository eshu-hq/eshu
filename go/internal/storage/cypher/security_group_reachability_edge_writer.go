// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// canonicalPhaseSecurityGroupRuleNode and the edge phases name the
// security-group reachability projection phases for grouped-backend statement
// metadata and diagnostics (issue #1135 PR2b).
const (
	canonicalPhaseSecurityGroupRuleNode = "security_group_rule_node"
	canonicalPhaseSecurityGroupSGEdge   = "security_group_sg_rule_edge"
	canonicalPhaseSecurityGroupToEdge   = "security_group_rule_endpoint_edge"
)

// securityGroupRuleNodeLabel is the static label for the reachability rule node
// and the bounded entity-label tag for its statement metadata.
const securityGroupRuleNodeLabel = "SecurityGroupRule"

// securityGroupAllowsRelTypeVocabulary is the closed set of Cypher relationship
// types the SecurityGroup -> SecurityGroupRule edge may use. Direction picks the
// member; the type is interpolated into the relationship-type position (which
// cannot be parameterized), so a value outside this set is rejected rather than
// turned into an arbitrary relationship type. The members mirror the reducer's
// securityGroupAllows*RelType vocabulary; the duplication is intentional because
// the cypher writer owns the relationship-type position and must not depend on
// reducer internals.
var securityGroupAllowsRelTypeVocabulary = map[string]struct{}{
	"ALLOWS_INGRESS": {},
	"ALLOWS_EGRESS":  {},
}

// securityGroupEndpointLabelVocabulary is the closed set of node labels the
// SecurityGroupRule -[:TO]-> endpoint edge may anchor its target on. The label is
// heterogeneous (the rule's source family decides it) but bounded to nodes a
// prerequisite materialization already committed: CidrBlock and PrefixList (#1135
// PR2a) and the SecurityGroup CloudResource (#805). Each is in the schema's
// uidConstraintLabels, so each target MATCH is an indexed uid anchor. The label
// is interpolated into the node-label position, so a value outside this set is
// rejected rather than turned into an arbitrary MATCH label.
var securityGroupEndpointLabelVocabulary = map[string]struct{}{
	"CidrBlock":     {},
	"CloudResource": {},
	"PrefixList":    {},
}

// canonicalSecurityGroupRuleNodeUpsertCypher batches :SecurityGroupRule node
// upserts. MERGE is on the stable port-precise uid identity only (a deterministic
// hash of the SG anchor uid, direction, protocol, normalized port range, and
// source); mutable properties are SET separately so duplicate input rows and
// reducer retries converge on one node. Port and protocol live as NODE properties
// keyed in the uid (Option D), never in a relationship-property MERGE that times
// out at 20s on NornicDB (#805 §5.3).
const canonicalSecurityGroupRuleNodeUpsertCypher = `UNWIND $rows AS row
MERGE (r:SecurityGroupRule {uid: row.uid})
SET r.id = row.uid,
    r.sg_uid = row.sg_uid,
    r.direction = row.direction,
    r.ip_protocol = row.ip_protocol,
    r.from_port = row.from_port,
    r.to_port = row.to_port,
    r.name = row.name,
    r.source_kind = row.source_kind,
    r.is_internet = row.is_internet,
    r.source_fact_id = row.source_fact_id,
    r.stable_fact_key = row.stable_fact_key,
    r.source_system = row.source_system,
    r.source_record_id = row.source_record_id,
    r.collector_kind = row.collector_kind,
    r.evidence_source = row.evidence_source`

// canonicalSecurityGroupSGRuleEdgeUpsertCypherFormat batches the
// SecurityGroup -> SecurityGroupRule edge upsert. The relationship type is a
// validated static token from the closed direction vocabulary (the single %s).
// Two MATCHes precede the MERGE so a row whose SG CloudResource node or
// SecurityGroupRule node is absent produces no edge and no fabricated node.
const canonicalSecurityGroupSGRuleEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (sg:CloudResource {uid: row.sg_uid})
MATCH (rule:SecurityGroupRule {uid: row.rule_uid})
MERGE (sg)-[rel:%s]->(rule)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// canonicalSecurityGroupToEdgeUpsertCypherFormat batches the
// SecurityGroupRule -[:TO]-> endpoint edge upsert. The relationship type is the
// static TO token; the target-node label is a validated static token from the
// closed endpoint vocabulary (the single %s). Two MATCHes precede the MERGE so a
// row whose rule or endpoint node is absent produces no edge and no fabricated
// node.
const canonicalSecurityGroupToEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (rule:SecurityGroupRule {uid: row.rule_uid})
MATCH (target:%s {uid: row.target_uid})
MERGE (rule)-[rel:TO]->(target)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractSecurityGroupSGRuleEdgesCypher removes this reducer's SG -> rule edges
// for a set of scopes before a fresh generation reprojects them. The direction
// relationship types are closed, so the retract matches any relationship from a
// CloudResource to a SecurityGroupRule and scopes by the edge's own scope_id and
// evidence_source. CloudResource and SecurityGroupRule nodes are cross-generation
// canonical and carry no reducer scope_id, so a node-scoped predicate would leak
// stale edges.
const retractSecurityGroupSGRuleEdgesCypher = `MATCH (sg:CloudResource)-[rel]->(rule:SecurityGroupRule)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// retractSecurityGroupToEdgesCypher removes this reducer's rule -> endpoint TO
// edges for a set of scopes. The TO type is fixed, so the retract matches it from
// any SecurityGroupRule and scopes by the edge's own scope_id and evidence_source.
const retractSecurityGroupToEdgesCypher = `MATCH (rule:SecurityGroupRule)-[rel:TO]->()
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// SecurityGroupReachabilityWriter materializes the Option D network-reachability
// graph (issue #1135 PR2b): idempotent :SecurityGroupRule nodes, the
// SecurityGroup -> SecurityGroupRule direction edges, and the
// SecurityGroupRule -[:TO]-> endpoint edges. It satisfies the reducer-owned
// reachability-writer consumer interface and writes through the backend-neutral
// Executor seam.
type SecurityGroupReachabilityWriter struct {
	executor  Executor
	batchSize int
}

// NewSecurityGroupReachabilityWriter returns a writer backed by the given
// Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewSecurityGroupReachabilityWriter(executor Executor, batchSize int) *SecurityGroupReachabilityWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &SecurityGroupReachabilityWriter{executor: executor, batchSize: batchSize}
}

// WriteSecurityGroupRuleNodes upserts :SecurityGroupRule nodes for the given rows
// using batched UNWIND statements. The write is idempotent: the same uid converges
// on one node across batches, retries, and generations. evidenceSource is stamped
// on every node so the prior-generation node retract (a follow-up slice) can scope
// to reducer-owned rule nodes.
func (w *SecurityGroupReachabilityWriter) WriteSecurityGroupRuleNodes(
	ctx context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("security group reachability writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		annotated = append(annotated, cloneRowWith(row, map[string]any{"evidence_source": evidenceSource}))
	}

	stmts := buildBatchedStatements(canonicalSecurityGroupRuleNodeUpsertCypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseSecurityGroupRuleNode
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = securityGroupRuleNodeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d",
			securityGroupRuleNodeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// WriteSecurityGroupSGRuleEdges upserts the SecurityGroup -> SecurityGroupRule
// edges for the given rows, grouped by the validated direction relationship type,
// using batched MATCH-MATCH-MERGE statements. The write is idempotent: the same
// (sg_uid, relationship_type, rule_uid) converges on one edge, and a missing
// endpoint is a no-op rather than a fabricated node. scopeID/generationID/
// evidenceSource are stamped on every edge; rel.scope_id is what the
// prior-generation retract filters on (the endpoint nodes carry no scope_id).
func (w *SecurityGroupReachabilityWriter) WriteSecurityGroupSGRuleEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	return w.writeTokenGroupedEdges(
		ctx,
		rows,
		scopeID,
		generationID,
		evidenceSource,
		"relationship_type",
		canonicalSecurityGroupSGRuleEdgeUpsertCypherFormat,
		canonicalPhaseSecurityGroupSGEdge,
		validateSecurityGroupAllowsRelType,
	)
}

// WriteSecurityGroupRuleEndpointEdges upserts the SecurityGroupRule -[:TO]->
// endpoint edges for the given rows, grouped by the validated target-node label,
// using batched MATCH-MATCH-MERGE statements. The write is idempotent: the same
// (rule_uid, TO, target_uid) converges on one edge, and a missing endpoint is a
// no-op rather than a fabricated node.
func (w *SecurityGroupReachabilityWriter) WriteSecurityGroupRuleEndpointEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	return w.writeTokenGroupedEdges(
		ctx,
		rows,
		scopeID,
		generationID,
		evidenceSource,
		"target_label",
		canonicalSecurityGroupToEdgeUpsertCypherFormat,
		canonicalPhaseSecurityGroupToEdge,
		validateSecurityGroupEndpointLabel,
	)
}

// writeTokenGroupedEdges is the shared batched-upsert path for both reachability
// edge families. Rows are grouped by a validated static token (the direction
// relationship type or the endpoint node label), each group's token is
// interpolated into one position of the Cypher template, and all batches dispatch
// as one atomic group. Keeping one implementation avoids drift between the two
// edge writers and centralizes the closed-vocabulary screen.
func (w *SecurityGroupReachabilityWriter) writeTokenGroupedEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
	tokenField string,
	cypherFormat string,
	phase string,
	validateToken func(map[string]any) (string, error),
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("security group reachability writer executor is required")
	}

	grouped := make(map[string][]map[string]any)
	tokens := make([]string, 0, len(rows))
	for _, row := range rows {
		token, err := validateToken(row)
		if err != nil {
			return err
		}
		cloned := cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		})
		if _, exists := grouped[token]; !exists {
			tokens = append(tokens, token)
		}
		grouped[token] = append(grouped[token], cloned)
	}
	sort.Strings(tokens)

	var stmts []Statement
	for _, token := range tokens {
		cypher := fmt.Sprintf(cypherFormat, token)
		batches := buildBatchedStatements(cypher, grouped[token], w.batchSize)
		for index := range batches {
			batchRows := batches[index].Parameters["rows"].([]map[string]any)
			batches[index].Parameters[StatementMetadataPhaseKey] = phase
			batches[index].Parameters[StatementMetadataEntityLabelKey] = securityGroupRuleNodeLabel
			batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
				"phase=%s token=%s rows=%d",
				phase,
				token,
				len(batchRows),
			)
		}
		stmts = append(stmts, batches...)
	}

	return w.dispatch(ctx, stmts)
}

// RetractSecurityGroupReachability removes this reducer's reachability edges (both
// the SG -> rule and rule -> endpoint TO families) for the given scopes before a
// fresh generation reprojects them, as one atomic group. It is a no-op for an
// empty scope set. The delete is scoped to the reducer's evidence_source and
// never touches endpoint, SG, or rule nodes.
func (w *SecurityGroupReachabilityWriter) RetractSecurityGroupReachability(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("security group reachability writer executor is required")
	}

	stmts := make([]Statement, 0, 2)
	for _, spec := range []struct {
		cypher string
		phase  string
	}{
		{retractSecurityGroupSGRuleEdgesCypher, canonicalPhaseSecurityGroupSGEdge},
		{retractSecurityGroupToEdgesCypher, canonicalPhaseSecurityGroupToEdge},
	} {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    spec.cypher,
			Parameters: map[string]any{
				"scope_ids":                     scopeIDs,
				"evidence_source":               evidenceSource,
				StatementMetadataPhaseKey:       spec.phase,
				StatementMetadataEntityLabelKey: securityGroupRuleNodeLabel,
				StatementMetadataSummaryKey: fmt.Sprintf(
					"phase=%s retract scopes=%d generation=%s",
					spec.phase,
					len(scopeIDs),
					generationID,
				),
			},
		})
	}

	return w.dispatch(ctx, stmts)
}

// validateSecurityGroupAllowsRelType screens a row's relationship_type against
// the closed direction vocabulary and the static-token character class before it
// is interpolated into the relationship-type position. Membership is checked, not
// just the character class, so a charset-safe but out-of-vocabulary token (or
// injected text) can never reach the relationship-type position.
func validateSecurityGroupAllowsRelType(row map[string]any) (string, error) {
	return validateStaticGraphToken(row, "relationship_type", securityGroupAllowsRelTypeVocabulary, "security group allows relationship_type")
}

// validateSecurityGroupEndpointLabel screens a row's target_label against the
// closed endpoint vocabulary and the static-token character class before it is
// interpolated into the node-label position.
func validateSecurityGroupEndpointLabel(row map[string]any) (string, error) {
	return validateStaticGraphToken(row, "target_label", securityGroupEndpointLabelVocabulary, "security group endpoint target_label")
}

// validateStaticGraphToken runs the character-class screen first (to keep the
// error for unsafe input precise) then the closed-vocabulary allowlist check,
// returning the token for interpolation. This is the single enforcement point for
// the schema-surface contract on the direction relationship type and the
// heterogeneous endpoint label.
func validateStaticGraphToken(row map[string]any, field string, vocabulary map[string]struct{}, label string) (string, error) {
	raw, ok := row[field].(string)
	if !ok || raw == "" || raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("%s must be a non-empty string", label)
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "", fmt.Errorf("%s %q contains unsupported character %q", label, raw, ch)
	}
	if _, ok := vocabulary[raw]; !ok {
		return "", fmt.Errorf("%s %q is outside the closed vocabulary", label, raw)
	}
	return raw, nil
}

// cloneRowWith returns a shallow copy of row with the extra annotations applied,
// so the writer never mutates the caller's resolved row maps.
func cloneRowWith(row map[string]any, annotations map[string]any) map[string]any {
	cloned := make(map[string]any, len(row)+len(annotations))
	for key, value := range row {
		cloned[key] = value
	}
	for key, value := range annotations {
		cloned[key] = value
	}
	return cloned
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *SecurityGroupReachabilityWriter) dispatch(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
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
