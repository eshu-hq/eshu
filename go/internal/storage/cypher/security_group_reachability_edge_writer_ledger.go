// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// retractSecurityGroupSGRuleEdgesByUIDsCypher is the ledger-anchored
// counterpart of retractSecurityGroupSGRuleEdgesCypher: it enumerates source
// CloudResource uids via a `WHERE sg.uid IN $source_uids` predicate instead of
// scanning the whole :CloudResource label, mirroring
// retractCloudResourceEdgesByUIDsCypher (issue #4858, #4881). The
// `sg:CloudResource` label on the MATCH restricts this statement to the
// SG->rule edge family even though it shares the $source_uids parameter with
// retractSecurityGroupToEdgesByUIDsCypher: a rule uid in the same batch simply
// matches no :CloudResource node and contributes no deletes from this
// statement.
const retractSecurityGroupSGRuleEdgesByUIDsCypher = `MATCH (sg:CloudResource)-[rel]->(:SecurityGroupRule)
WHERE sg.uid IN $source_uids
  AND rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// retractSecurityGroupToEdgesByUIDsCypher is the ledger-anchored counterpart of
// retractSecurityGroupToEdgesCypher: it enumerates source SecurityGroupRule
// uids via a `WHERE rule.uid IN $source_uids` predicate instead of scanning
// every SecurityGroupRule node's TO edges. The `rule:SecurityGroupRule` label
// on the MATCH restricts this statement to the rule->endpoint edge family even
// though it shares the $source_uids parameter with
// retractSecurityGroupSGRuleEdgesByUIDsCypher: an SG CloudResource uid in the
// same batch simply matches no :SecurityGroupRule node and contributes no
// deletes from this statement.
const retractSecurityGroupToEdgesByUIDsCypher = `MATCH (rule:SecurityGroupRule)-[rel:TO]->()
WHERE rule.uid IN $source_uids
  AND rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// securityGroupReachabilityRetractUIDBatchSize bounds the number of source
// uids passed in the $source_uids IN-list per anchored-retract statement,
// mirroring cloudResourceEdgeRetractUIDBatchSize. The same batch of uids (a
// mix of SG CloudResource uids and SecurityGroupRule uids) is reused for both
// the SG->rule and rule->endpoint anchored-retract statements.
const securityGroupReachabilityRetractUIDBatchSize = 500

// RetractSecurityGroupReachabilityByUIDs removes this reducer's reachability
// edges (both the SG -> rule and rule -> endpoint TO families) for the given
// scopes, enumerating source uids from the projected-source ledger instead of
// scanning the whole :CloudResource / :SecurityGroupRule labels. Both families
// are anchored on the SAME sourceUIDs set — a single ledger tracks the union
// of SG CloudResource uids (the SG->rule edge source) and SecurityGroupRule
// uids (the rule->endpoint edge source) for this evidence_source — and each
// retract statement's own source-node label filters the batch down to the
// uids that are actually its source family, so an SG uid never matches the
// rule->endpoint statement and vice versa. It is a no-op for an empty uid set.
// The delete is scoped to the reducer's evidence_source and never touches
// endpoint, SG, or rule nodes.
func (w *SecurityGroupReachabilityWriter) RetractSecurityGroupReachabilityByUIDs(
	ctx context.Context,
	sourceUIDs []string,
	scopeIDs []string,
	evidenceSource string,
) error {
	if len(sourceUIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("security group reachability writer executor is required")
	}

	batches := chunkStrings(sourceUIDs, securityGroupReachabilityRetractUIDBatchSize)
	stmts := make([]Statement, 0, len(batches)*2)
	for _, batch := range batches {
		for _, spec := range []struct {
			cypher string
			phase  string
		}{
			{retractSecurityGroupSGRuleEdgesByUIDsCypher, canonicalPhaseSecurityGroupSGEdge},
			{retractSecurityGroupToEdgesByUIDsCypher, canonicalPhaseSecurityGroupToEdge},
		} {
			stmts = append(stmts, Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    spec.cypher,
				Parameters: map[string]any{
					"source_uids":                   batch,
					"scope_ids":                     scopeIDs,
					"evidence_source":               evidenceSource,
					StatementMetadataPhaseKey:       spec.phase,
					StatementMetadataEntityLabelKey: securityGroupRuleNodeLabel,
					StatementMetadataSummaryKey: fmt.Sprintf(
						"phase=%s retract_by_uids scopes=%d uids=%d",
						spec.phase,
						len(scopeIDs),
						len(batch),
					),
				},
			})
		}
	}
	return w.dispatchRetract(ctx, stmts)
}

// dispatchRetract routes anchored-retract statements through sequential
// Execute calls, never ExecuteGroup. This avoids a NornicDB v1.1.9 bolt driver
// bug where UNWIND … MATCH … -[rel]-> … DELETE rel inside session.ExecuteWrite
// / tx.Run returns zero rows (the MATCH on the relationship finds nothing),
// even though the same statement via session.Run (autocommit) produces
// correct results. See cypher.CloudResourceEdgeWriter.dispatchRetract for the
// same rationale applied to the CloudResource anchored retract.
func (w *SecurityGroupReachabilityWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
