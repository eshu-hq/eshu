// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// Route V (#10880 PR-B2a, arbiter ruling V and its index amendment) serves an
// unscoped profile=precise call from the winners read plus a supplement.
//
// A winner is the best eligible support of its key (same eligibility, same
// order as the supports read), so a winner that passes the precise filter IS the
// filter-then-pick answer for its key: that is branch A, the shipped winners read
// with the precise predicate on the winner row. A winner that fails the filter
// says nothing about the key's other supports, which can differ in match reason
// and in the stamped detection profile. Branch B recovers those keys: the winners
// rows that match the key-determined selectors and fail the filter probe the best
// precise-eligible support of their key through the partial index
// supply_chain_impact_finding_supports_precise_key_ordered_idx (migration 343).
// The two branches are one statement (UNION ALL), so one read sees one snapshot.
//
// Branch B rules (ruling decision 2):
//   - candidate keys are chosen on the winner row only by key-determined
//     selectors (cve, advisory, package, repository, subject digest, status) and
//     the runtime filters, which key on the repository;
//   - the status prune keeps winners that can have a precise support (the status
//     and the observed version are key parts);
//   - min_priority_score may prune on the winner row (a support never outranks its
//     key's winner) and is applied again to the pick;
//   - severity, priority bucket and ecosystem apply to the PICKED support, never to
//     the winner row, because they are per-support columns;
//   - suppression uses the operator override probe on the picked support;
//   - the probe has an explicit ORDER BY and LIMIT 1 and never relies on the
//     index order for the answer. Eligibility is scalar subqueries, not joins: a
//     join form made the planner drive from the scope tables (measured 137 s
//     against 0.8 s, docs/internal/evidence/10880-pr-b2a/RESULTS.md).

// supplementBinds names the positional parameters one route V statement binds.
// The list uses the list numbering and the aggregates and grouped inventory the
// aggregate numbering, so the supplement is written once.
type supplementBinds struct {
	cve, pkg, repo, digest, status, advisory, ecosystem string
	service, workload, environment                      string
	severity, bucket, minScore, suppState               string
	includeSuppressed, readAt                           string
}

// supportsEligibleCopySQL is the read-time eligibility of one supports row aliased
// alias, written as AND-ed lines: not an operator row, in the active generation of
// its scope, not an associated or derived-image finding, and not a classified
// finding that is anything but static and active. It mirrors the winners store's
// source candidate eligibility (the shortcut invariant test holds the two to the
// same picks), and the explain identity lookup reuses it so a derived or inactive
// copy never resolves a key.
func supportsEligibleCopySQL(alias string) string {
	return `      AND ` + alias + `.scope_id <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
      AND COALESCE((SELECT TRUE
                    FROM ingestion_scopes AS sc
                    JOIN scope_generations AS gen
                      ON gen.scope_id = sc.scope_id AND gen.generation_id = sc.active_generation_id
                    WHERE sc.scope_id = ` + alias + `.scope_id AND sc.active_generation_id = ` + alias + `.generation_id
                      AND gen.status = 'active'
                    LIMIT 1), FALSE)
      AND NOT EXISTS (SELECT 1 FROM scanner_image_result_fact_associations AS association
                      WHERE association.finding_fact_id = ` + alias + `.fact_id)
      AND ` + alias + `.scope_id NOT LIKE 'image://derived/%'
      AND COALESCE((SELECT class.source_class = 'static' AND class.lifecycle = 'active'
                    FROM scanner_image_result_fact_classifications AS class
                    WHERE class.finding_fact_id = ` + alias + `.fact_id
                    LIMIT 1), TRUE)`
}

// supplementProbe returns the best precise-eligible support of the candidate key
// c.canonical_key as one row aliased source.
var supplementProbe = `CROSS JOIN LATERAL (
    SELECT s.fact_id, s.finding_id, s.canonical_key, s.priority_score, s.impact_status,
           s.priority_bucket, s.severity_bucket, s.repository_id, s.ecosystem, s.suppression_state
    FROM supply_chain_impact_finding_supports AS s
    WHERE s.canonical_key = c.canonical_key
      AND ` + supplyChainImpactPrecisePredicateSQL("s") + `
` + supportsEligibleCopySQL("s") + `
    ORDER BY s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC
    LIMIT 1
  ) AS source`

// supplementCTEs returns the two CTEs of branch B, each preceded by a comma so
// the caller appends them to its chain: b_candidates (the winners rows whose
// winner fails the precise filter) and branch_b (their picks, rank and
// suppression filters applied to the pick). branch_b exposes finding_id,
// canonical_key, fact_id, priority_score, impact_status, priority_bucket,
// severity_bucket, repository_id, ecosystem, override_fact_id and
// effective_suppression_state.
func supplementCTEs(b supplementBinds) string {
	effective := supplyChainImpactEffectiveDecisionStateSQL("override.suppression_state", "override.expires_at", b.readAt+"::timestamptz")
	text := `,
b_candidates AS (
  SELECT w.canonical_key
  FROM supply_chain_impact_canonical_winners AS w
  WHERE ` + supplyChainImpactPreciseStatusPruneSQL("w") + `
    AND NOT ` + supplyChainImpactPrecisePredicateSQL("w") +
		supplyChainImpactStaticWinnerPredicateSQL + `
    AND ({cve} = '' OR w.cve_id = {cve})
    AND ({pkg} = '' OR w.package_id = {pkg})
    AND ({repo} = '' OR w.repository_id = {repo})
    AND ({digest} = '' OR w.subject_digest = {digest})
    AND ({status} = '' OR w.impact_status = {status})
    AND ({advisory} = '' OR w.advisory_id = {advisory})
` + supplyChainImpactRuntimeFilterPredicate("w.repository_id", b.service, b.workload, b.environment) + `
    AND ({minScore} = 0 OR w.priority_score >= {minScore})
),
branch_b AS (
  SELECT source.finding_id, source.canonical_key, source.fact_id, source.priority_score,
         source.impact_status, source.priority_bucket, source.severity_bucket,
         source.repository_id, source.ecosystem,
         override.fact_id AS override_fact_id, effective.state AS effective_suppression_state
  FROM b_candidates AS c
  ` + supplementProbe + `
  ` + supportsListOverrideLateral + `
  CROSS JOIN LATERAL (
    SELECT CASE WHEN override.fact_id IS NULL THEN source.suppression_state ELSE ` + effective + ` END AS state
  ) AS effective
  WHERE ({ecosystem} = '' OR LOWER((source.ecosystem) COLLATE "C") COLLATE "default" = LOWER(({ecosystem}) COLLATE "C") COLLATE "default")
    AND ({severity} = '' OR source.severity_bucket = {severity})
    AND ({bucket} = '' OR source.priority_bucket = {bucket})
    AND ({minScore} = 0 OR source.priority_score >= {minScore})
    AND ({suppState} = '' OR effective.state = {suppState})
    AND ({includeSuppressed}::boolean OR effective.state NOT IN (` + supplyChainImpactHiddenSuppressionStatesSQL + `))
)`
	// The static-winner predicate carries the protected-reads marker; substitute it
	// closed here because the list derivation starts from text that was already
	// substituted (the aggregate and grouped derivations substitute again, which is
	// a no-op).
	return supplyChainImpactReadModeSQL(strings.NewReplacer(
		"{cve}", b.cve, "{pkg}", b.pkg, "{repo}", b.repo, "{digest}", b.digest, "{status}", b.status,
		"{advisory}", b.advisory, "{ecosystem}", b.ecosystem, "{severity}", b.severity,
		"{bucket}", b.bucket, "{minScore}", b.minScore, "{suppState}", b.suppState,
		"{includeSuppressed}", b.includeSuppressed,
	).Replace(text), false)
}

// replaceOnce replaces the single occurrence of old in text, or returns an error
// naming it, so a derivation fails at package initialization when the shipped
// text it was written against moves.
func replaceOnce(text, old, replacement, what string) (string, error) {
	if strings.Count(text, old) != 1 {
		return "", &derivationError{what: what}
	}
	return strings.Replace(text, old, replacement, 1), nil
}

type derivationError struct{ what string }

func (e *derivationError) Error() string {
	return "route V derivation lost the marker " + e.what + ": update the supplement derivation"
}
