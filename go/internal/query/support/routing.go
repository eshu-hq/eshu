// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import "strings"

// The PagerDuty incident-routing fact kinds a story target-support read can link
// to a repository, and the reducer fact the link goes through.
const (
	// AppliedPagerDutyResourceKind is a Terraform-state applied PagerDuty
	// resource. Only resource_class "service" carries a service id.
	AppliedPagerDutyResourceKind = "incident_routing.applied_pagerduty_resource"
	// ObservedPagerDutyServiceKind is a service the PagerDuty collector read live.
	ObservedPagerDutyServiceKind = "incident_routing.observed_pagerduty_service"
	// IncidentCorrelationKind is the reducer's incident-repository correlation
	// decision for one provider service.
	IncidentCorrelationKind = "reducer_incident_repository_correlation"

	// pagerDutyProvider is the only provider whose service ids the two routing
	// kinds above carry.
	pagerDutyProvider = "pagerduty"
	// appliedServiceClass is the applied resource class that names a service.
	appliedServiceClass = "service"

	// AdmissibleCorrelationsCTE is the name of the common table expression the
	// source-only statement defines with AdmissibleCorrelationsSQL.
	AdmissibleCorrelationsCTE = "admissible_correlations"
)

// AppliedServiceKey is the provider service id of an applied PagerDuty resource,
// written as fact_records_incident_routing_applied_service_idx (migration 003)
// spells its key, so a probe on it is an index condition and never a heap filter.
func AppliedServiceKey(alias string) string {
	return alias + ".payload->>'provider_object_id'"
}

// ObservedServiceKey is the provider service id of an observed PagerDuty service
// (provider_object_id, falling back to service_id for a legacy payload), written
// as fact_records_incident_routing_observed_service_idx spells its key.
func ObservedServiceKey(alias string) string {
	return "COALESCE(NULLIF(" + alias + ".payload->>'provider_object_id', ''), " + alias + ".payload->>'service_id', '')"
}

// admissibleCorrelationFilter keeps the reducer correlation facts the read may
// join through: an exact or derived decision that is not provenance-only, made
// for PagerDuty, and carrying a provider service id. It mirrors
// storage/postgres/service_incident_evidence_loader.go, and a correlation from
// another provider that reuses an id never links a PagerDuty fact.
func admissibleCorrelationFilter(alias string) string {
	return `    AND ` + alias + `.payload->>'provenance_only' = 'false'
    AND ` + alias + `.payload->>'outcome' IN ('exact', 'derived')
    AND COALESCE(NULLIF(` + alias + `.payload->>'provider', ''), '` + pagerDutyProvider + `') = '` + pagerDutyProvider + `'`
}

// IncidentRoutingSQL renders the bounded statement that reads the PagerDuty
// routing facts correlated to repositoryID and returns "" when repositoryID is
// blank. $1 is the repository id and $2 the row limit; the caller binds
// limit+1 so truncation is visible.
//
// The statement is the repository-first shape the #7463 plan proof measured, not
// a join of fact_records to the active scope and generation. Written as a plain
// join, Postgres drove from the 242 active generations and read every observed
// service of every one of them (40,006 rows, 60k buffer hits, 30 to 64 ms at one
// million facts). Instead:
//
//  1. the correlation facts of repositoryID are probed through
//     fact_records_incident_repository_correlation_service_idx, keyed on
//     repository_id, and kept only on an active generation;
//  2. each correlated provider service id is probed through the applied and
//     observed service indexes by LATERAL, fenced by OFFSET 0 so the planner
//     cannot flatten it back into a join;
//  3. the routing fact's own generation must be active.
//
// Every row carries a correlation object naming the repository and provider
// service the join went through, which RoutingFactCorrelatedTo re-checks.
func IncidentRoutingSQL(repositoryID string, limit int) (string, []any) {
	if strings.TrimSpace(repositoryID) == "" {
		return "", nil
	}
	return `
WITH correlated AS MATERIALIZED (
  SELECT DISTINCT cand.provider_service_id
  FROM (
    SELECT corr.scope_id, corr.generation_id, corr.payload->>'provider_service_id' AS provider_service_id
    FROM fact_records AS corr
    WHERE corr.fact_kind = '` + IncidentCorrelationKind + `'
      AND corr.is_tombstone = FALSE
      AND corr.payload->>'repository_id' = $1
` + admissibleCorrelationFilter("corr") + `
    OFFSET 0
  ) AS cand
  JOIN ingestion_scopes AS cscope
    ON cscope.scope_id = cand.scope_id
   AND cscope.active_generation_id = cand.generation_id
  JOIN scope_generations AS cgen
    ON cgen.scope_id = cand.scope_id
   AND cgen.generation_id = cand.generation_id
   AND cgen.status = 'active'
  WHERE NULLIF(cand.provider_service_id, '') IS NOT NULL
), routed AS (
  SELECT probe.fact_id, probe.fact_kind, probe.scope_id, probe.generation_id, probe.source_system,
         probe.source_record_id, probe.observed_at, probe.payload, correlated.provider_service_id
  FROM correlated
  CROSS JOIN LATERAL (
    SELECT ` + routedFactColumns + `
    FROM fact_records AS fact
    WHERE fact.fact_kind = '` + AppliedPagerDutyResourceKind + `'
      AND fact.is_tombstone = FALSE
      AND fact.payload->>'resource_class' = '` + appliedServiceClass + `'
      AND ` + AppliedServiceKey("fact") + ` = correlated.provider_service_id
    OFFSET 0
  ) AS probe
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = probe.scope_id AND scope.active_generation_id = probe.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = probe.scope_id
   AND generation.generation_id = probe.generation_id
   AND generation.status = 'active'
  UNION ALL
  SELECT probe.fact_id, probe.fact_kind, probe.scope_id, probe.generation_id, probe.source_system,
         probe.source_record_id, probe.observed_at, probe.payload, correlated.provider_service_id
  FROM correlated
  CROSS JOIN LATERAL (
    SELECT ` + routedFactColumns + `
    FROM fact_records AS fact
    WHERE fact.fact_kind = '` + ObservedPagerDutyServiceKind + `'
      AND fact.is_tombstone = FALSE
      AND ` + ObservedServiceKey("fact") + ` = correlated.provider_service_id
    OFFSET 0
  ) AS probe
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = probe.scope_id AND scope.active_generation_id = probe.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = probe.scope_id
   AND generation.generation_id = probe.generation_id
   AND generation.status = 'active'
)
SELECT jsonb_build_object(
    'fact_id', routed.fact_id,
    'fact_kind', routed.fact_kind,
    'scope_id', routed.scope_id,
    'generation_id', routed.generation_id,
    'source_system', routed.source_system,
    'source_record_id', routed.source_record_id,
    'observed_at', routed.observed_at,
    'payload', routed.payload,
    'correlation', jsonb_build_object(
        'repository_id', $1::text,
        'provider_service_id', routed.provider_service_id
    )
) AS payload
FROM routed
ORDER BY routed.observed_at DESC, routed.fact_id DESC
LIMIT $2
`, []any{strings.TrimSpace(repositoryID), limit + 1}
}

// routedFactColumns are the fact columns the routing read projects.
const routedFactColumns = `fact.fact_id,
           fact.fact_kind,
           fact.scope_id,
           fact.generation_id,
           fact.source_system,
           fact.source_record_id,
           fact.observed_at,
           fact.payload`

// AdmissibleCorrelationsSQL is the WITH prefix of the source-only statement: the
// set of provider service ids the reducer correlated, on an active generation,
// to any repository. The candidates are their own MATERIALIZED expression so
// they are computed once; folded into the join below, Postgres rescanned them
// for each of the 242 active scopes and discarded 4.1 million rows in a join
// filter (1.3 to 1.7 s at one million facts, against 41 to 85 ms materialized).
func AdmissibleCorrelationsSQL() string {
	return `WITH correlation_candidates AS MATERIALIZED (
  SELECT corr.scope_id, corr.generation_id, corr.payload->>'provider_service_id' AS provider_service_id
  FROM fact_records AS corr
  WHERE corr.fact_kind = '` + IncidentCorrelationKind + `'
    AND corr.is_tombstone = FALSE
` + admissibleCorrelationFilter("corr") + `
), ` + AdmissibleCorrelationsCTE + ` AS MATERIALIZED (
  SELECT DISTINCT cand.provider_service_id
  FROM correlation_candidates AS cand
  JOIN ingestion_scopes AS cscope
    ON cscope.scope_id = cand.scope_id AND cscope.active_generation_id = cand.generation_id
  JOIN scope_generations AS cgen
    ON cgen.scope_id = cand.scope_id AND cgen.generation_id = cand.generation_id AND cgen.status = 'active'
  WHERE NULLIF(cand.provider_service_id, '') IS NOT NULL
)`
}

// LinkedIncidentRoutingPredicate is true for a routing fact whose provider
// service the reducer correlated to some repository: an applied PagerDuty
// service or an observed PagerDuty service whose id is in
// AdmissibleCorrelationsCTE. It is the routing half of "this support fact has a
// durable link to some target", so a fact linked to another repository is
// neither this target's evidence nor source-only.
//
// The predicate is two-valued. The class and the keys are wrapped in
// COALESCE(..., an empty string), and the set holds no NULL because
// AdmissibleCorrelationsSQL drops blank ids, so IN returns true or false and
// never UNKNOWN; a bare payload->>'provider_object_id' IN (...) would be UNKNOWN
// for a fact without the key and, under NOT, drop that fact from the count. The set is uncorrelated, so Postgres evaluates it as a
// hashed subplan once. An applied resource of any class other than "service" and
// the coverage warning are never linked.
func LinkedIncidentRoutingPredicate() string {
	return `(fact.fact_kind = '` + AppliedPagerDutyResourceKind + `'
      AND COALESCE(fact.payload->>'resource_class', '') = '` + appliedServiceClass + `'
      AND COALESCE(` + AppliedServiceKey("fact") + `, '') IN (SELECT c.provider_service_id FROM ` + AdmissibleCorrelationsCTE + ` AS c))
  OR (fact.fact_kind = '` + ObservedPagerDutyServiceKind + `'
      AND ` + ObservedServiceKey("fact") + ` IN (SELECT c.provider_service_id FROM ` + AdmissibleCorrelationsCTE + ` AS c))`
}

// RoutingFactCorrelatedTo re-checks in Go what IncidentRoutingSQL selected on: fact
// is an applied PagerDuty service or an observed PagerDuty service, and the
// correlation the row carries names repositoryID and the provider service id of
// the fact itself. A row that fails any of these is never evidence, whatever
// reached it.
func RoutingFactCorrelatedTo(fact map[string]any, repositoryID string) bool {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return false
	}
	payload, _ := fact["payload"].(map[string]any)
	var providerServiceID string
	switch fact["fact_kind"] {
	case AppliedPagerDutyResourceKind:
		if text(payload, "resource_class") != appliedServiceClass {
			return false
		}
		providerServiceID = text(payload, "provider_object_id")
	case ObservedPagerDutyServiceKind:
		providerServiceID = text(payload, "provider_object_id")
		if providerServiceID == "" {
			providerServiceID = text(payload, "service_id")
		}
	default:
		return false
	}
	correlation, _ := fact["correlation"].(map[string]any)
	return providerServiceID != "" &&
		text(correlation, "repository_id") == repositoryID &&
		text(correlation, "provider_service_id") == providerServiceID
}

// IsRoutingFact reports whether kind is one of the two PagerDuty routing kinds
// this package links to a repository.
func IsRoutingFact(kind string) bool {
	return kind == AppliedPagerDutyResourceKind || kind == ObservedPagerDutyServiceKind
}

// text returns the string value at key without trimming: the SQL joins on exact
// equality, so the re-check compares exactly too.
func text(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
