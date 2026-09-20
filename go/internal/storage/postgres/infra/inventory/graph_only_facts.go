// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"strconv"
	"strings"
)

// Graph-only label fact aggregates (#6843).
//
// CloudResource and TerraformStateResource have no content_entities row, so
// the entities table cannot serve them. Both labels do have per-resource
// fact truth in fact_records on the same path that feeds the graph writers,
// so unscoped reads aggregate facts instead of scanning whole labels:
//
//   - CloudResource: reducer_cloud_resource_identity (shared admission; only
//     uid-resolved resources persist, so ambiguous, unsupported, and
//     unresolved identities never reach this read) plus ec2_instance_posture
//     (EC2 instances deliberately never emit inventory facts, so the EC2
//     materializer is their only node path). Existence-gated posture
//     enrichers (identity, KMS, S3/internet exposure) create no nodes.
//   - TerraformStateResource: terraform_state_resource plus
//     terraform_state_provider_binding for the provider bucket. The projector
//     drops facts that fail typed decode (address is the only required
//     field) and blank addresses without persisting that decision, so this
//     read replicates those two validity predicates.
//
// Currency is live truth: only the current generation per scope counts (the
// ingestion_scopes.active_generation_id join every graph writer consumes),
// tombstoned facts are excluded. The label sets are disjoint from Labels, so
// these branches UNION ALL with the entities aggregate and the query layer
// sums the rows exactly as it sums graph branches today.
//
// Deduplication mirrors the graph's uid convergence. Admission fact ids hash
// (scope, generation, uid), so one row per uid per scope-generation exists
// by construction; EC2 facts dedupe by identity tuple with the
// max-source-order-key rule (observed_at, fact_id). Across scopes the #5007
// owner-ledger rule keeps the greatest source order key, with ties broken by
// fact id; the CTEs below implement exactly that. Cross-family uid sharing
// (the same uid admitted and posture-observed) is excluded by collector
// contract — EC2 instances never appear in inventory facts — so families
// union without a global dedup.
//
// Bucket parity with the graph branches is narrow by construction: neither
// writer SETs environment (TerraformStateResource upserts REMOVE stale
// content environment), so the environment bucket is the constant 'unknown';
// CloudResource nodes never carry provider, so their provider bucket is
// source_system; service_kind reaches CloudResource through provider and
// posture facts, joined per identity below.
const (
	graphOnlyAdmissionFactKind = "reducer_cloud_resource_identity"
	graphOnlyEC2FactKind       = "ec2_instance_posture"

	graphOnlyStateResourceFactKind = "terraform_state_resource"
	graphOnlyStateBindingFactKind  = "terraform_state_provider_binding"

	graphOnlyCloudLabel = "CloudResource"
	graphOnlyStateLabel = "TerraformStateResource"

	graphOnlyDefaultEC2ResourceType = "aws_ec2_instance"
)

// GraphOnlyLabels is the closed set of infra labels served from fact truth
// rather than the entities table. The query package pins that these, plus
// Labels, partition its taxonomy exactly.
var GraphOnlyLabels = []string{
	graphOnlyCloudLabel,
	graphOnlyStateLabel,
}

// partitionReadLabels splits requested labels into the entities-table set
// (Labels) and the fact-aggregate set (GraphOnlyLabels), keeping input order.
// Labels outside both sets serve no branch; callers that pass taxonomy
// labels always land in exactly one side.
func partitionReadLabels(labels []string) (entity []string, facts []string) {
	inLabels := make(map[string]struct{}, len(Labels))
	for _, label := range Labels {
		inLabels[label] = struct{}{}
	}
	// Deduplicate keeping first-seen order: a repeated graph-only label
	// would otherwise emit its CTEs twice (a duplicate-CTE-name SQL error),
	// and a repeated entity label would UNION ALL its branch twice
	// (silently doubled counts).
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		if _, ok := inLabels[label]; ok {
			entity = append(entity, label)
			continue
		}
		if label == graphOnlyCloudLabel || label == graphOnlyStateLabel {
			facts = append(facts, label)
		}
	}
	return entity, facts
}

// graphOnlyActiveGenJoin scopes fact rows to the current generation, mirroring
// the currency every graph writer consumes.
const graphOnlyActiveGenJoin = `JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id`

// graphOnlyCloudNodesCTE resolves one row per CloudResource uid: the admitted
// identity rows plus the EC2 posture rows, each reduced to its cross-scope
// winner by greatest source order key (observed_at, fact_id). service_kind
// comes from the row's own family: admission rows join their contributing
// provider facts per provider identity key (the loader's AWS arn, GCP
// full_resource_name, Azure arm_resource_id); EC2 rows carry it in payload.
// Conflicting services for one identity resolve to the lowest fact_id with a
// non-blank service; single-service identities agree exactly.
//
// provider_services resolves that join once per (scope, generation,
// identity) with a single DISTINCT ON pass. The per-admission-row LATERAL
// this replaces re-scanned every provider fact of the scope for every
// admission row (quadratic buffers, caught by EXPLAIN on the gate corpus);
// the DISTINCT ON ordering reproduces the lateral's winner exactly:
// lowest fact_id among non-blank-service rows, lowest fact_id overall when
// all are blank, kind-agnostic on textual identity collisions.
const graphOnlyProviderServicesCTE = `provider_services AS (
  SELECT DISTINCT ON (src.scope_id, src.generation_id, ` + providerIdentitySQL + `)
    src.scope_id AS scope_id,
    src.generation_id AS generation_id,
    ` + providerIdentitySQL + ` AS identity,
    src.payload->>'service_kind' AS service_kind
  FROM fact_records AS src
  WHERE src.fact_kind IN ('aws_resource', 'gcp_cloud_resource', 'azure_cloud_resource')
    AND src.is_tombstone = FALSE
    AND ` + providerIdentitySQL + ` <> ''
  ORDER BY src.scope_id, src.generation_id, ` + providerIdentitySQL + `,
    (btrim(src.payload->>'service_kind') = ''), src.fact_id ASC
)`

// providerIdentitySQL trims the provider fact's kind-dependent identity key.
// It is a SQL macro, not a stored function: inlining keeps the expression
// index-agnostic call sites identical to the pre-rewrite lateral predicate.
const providerIdentitySQL = `btrim(CASE src.fact_kind
  WHEN 'aws_resource' THEN src.payload->>'arn'
  WHEN 'gcp_cloud_resource' THEN src.payload->>'full_resource_name'
  WHEN 'azure_cloud_resource' THEN src.payload->>'arm_resource_id'
END)`

const graphOnlyCloudNodesCTE = `cloud_nodes AS (
  (SELECT DISTINCT ON (adm.uid)
    adm.uid AS uid,
    adm.source_system AS source_system,
    adm.resource_type AS resource_type,
    COALESCE(NULLIF(svc.service_kind, ''), '') AS service_kind
  FROM (
    SELECT fact.payload->>'cloud_resource_uid' AS uid,
      fact.source_system AS source_system,
      fact.payload->>'resource_type' AS resource_type,
      fact.scope_id AS scope_id,
      fact.generation_id AS generation_id,
      fact.payload->>'raw_identity' AS raw_identity,
      fact.observed_at AS observed_at,
      fact.fact_id AS fact_id
    FROM fact_records AS fact
    ` + graphOnlyActiveGenJoin + `
    WHERE fact.fact_kind = '` + graphOnlyAdmissionFactKind + `'
      AND fact.is_tombstone = FALSE
      AND fact.payload->>'cloud_resource_uid' IS NOT NULL
      AND fact.payload->>'cloud_resource_uid' <> ''
  ) AS adm
  LEFT JOIN provider_services AS svc
    ON svc.scope_id = adm.scope_id
   AND svc.generation_id = adm.generation_id
   AND svc.identity = btrim(adm.raw_identity)
  ORDER BY adm.uid, adm.observed_at DESC, adm.fact_id DESC)
  UNION ALL
  (SELECT DISTINCT ON (
      COALESCE(fact.payload->>'account_id', ''),
      COALESCE(fact.payload->>'region', ''),
      COALESCE(NULLIF(fact.payload->>'resource_type', ''), '` + graphOnlyDefaultEC2ResourceType + `'),
      COALESCE(NULLIF(fact.payload->>'instance_id', ''), fact.payload->>'arn')
    )
    '' AS uid,
    fact.source_system AS source_system,
    COALESCE(NULLIF(fact.payload->>'resource_type', ''), '` + graphOnlyDefaultEC2ResourceType + `') AS resource_type,
    COALESCE(fact.payload->>'service_kind', '') AS service_kind
  FROM fact_records AS fact
  ` + graphOnlyActiveGenJoin + `
  WHERE fact.fact_kind = '` + graphOnlyEC2FactKind + `'
    AND fact.is_tombstone = FALSE
    AND (
      COALESCE(fact.payload->>'instance_id', '') <> ''
      OR COALESCE(fact.payload->>'arn', '') <> ''
    )
  ORDER BY
    COALESCE(fact.payload->>'account_id', ''),
    COALESCE(fact.payload->>'region', ''),
    COALESCE(NULLIF(fact.payload->>'resource_type', ''), '` + graphOnlyDefaultEC2ResourceType + `'),
    COALESCE(NULLIF(fact.payload->>'instance_id', ''), fact.payload->>'arn'),
    fact.observed_at DESC,
    fact.fact_id DESC
  )
)`

// graphOnlyBindingFirstCTE resolves the bound provider_type once per
// (scope, generation, address) with a single DISTINCT ON pass. The
// per-resource LATERAL this replaces re-scanned every binding fact of the
// scope for every state resource (quadratic buffers, caught by EXPLAIN on
// the gate corpus); first-by-fact-id ordering reproduces the lateral's
// winner exactly.
const graphOnlyBindingFirstCTE = `binding_first AS (
  SELECT DISTINCT ON (bind.scope_id, bind.generation_id, btrim(bind.payload->>'resource_address'))
    bind.scope_id AS scope_id,
    bind.generation_id AS generation_id,
    btrim(bind.payload->>'resource_address') AS address,
    bind.payload->>'provider_type' AS provider_type
  FROM fact_records AS bind
  WHERE bind.fact_kind = '` + graphOnlyStateBindingFactKind + `'
    AND bind.is_tombstone = FALSE
    AND btrim(bind.payload->>'resource_address') <> ''
    AND btrim(bind.payload->>'provider_address') <> ''
  ORDER BY bind.scope_id, bind.generation_id, btrim(bind.payload->>'resource_address'),
    bind.fact_id ASC
)`

// graphOnlyStateNodesCTE resolves one row per state resource: valid resource
// facts (decodable address, non-blank after trim) reduced to the greatest
// source order key per scope, generation, and address, with the bound
// provider_type from first-by-fact-id decodable bindings.
const graphOnlyStateNodesCTE = `state_nodes AS (
  SELECT winner.address AS address,
    binding.provider_type AS provider,
    winner.resource_type AS resource_type
  FROM (
    SELECT DISTINCT ON (
        fact.scope_id,
        fact.generation_id,
        btrim(fact.payload->>'address')
      )
      btrim(fact.payload->>'address') AS address,
      fact.payload->>'type' AS resource_type,
      fact.scope_id AS scope_id,
      fact.generation_id AS generation_id
    FROM fact_records AS fact
    ` + graphOnlyActiveGenJoin + `
    WHERE fact.fact_kind = '` + graphOnlyStateResourceFactKind + `'
      AND fact.is_tombstone = FALSE
      AND btrim(fact.payload->>'address') <> ''
    ORDER BY
      fact.scope_id,
      fact.generation_id,
      btrim(fact.payload->>'address'),
      fact.observed_at DESC,
      fact.fact_id DESC
  ) AS winner
  LEFT JOIN binding_first AS binding
    ON binding.scope_id = winner.scope_id
   AND binding.generation_id = winner.generation_id
   AND binding.address = winner.address
)`

// graphOnlyCloudWhere renders the cloud branch predicates over cloud_nodes,
// mirroring the graph branch clauses: kind matches resource_type (plus
// service_kind when the all-categories service arm can reach cloud), provider
// matches source_system, and environment/category filters that no
// CloudResource node can satisfy render unsatisfiable rather than silently
// widening.
func graphOnlyCloudWhere(f Filter, param func(string) string) []string {
	clauses := []string{`TRUE`}
	if f.Kind != "" {
		p := param(f.Kind)
		if f.AllCategories {
			clauses = append(clauses, `(cloud_nodes.resource_type = `+p+` OR cloud_nodes.service_kind = `+p+`)`)
		} else {
			clauses = append(clauses, `cloud_nodes.resource_type = `+p)
		}
	}
	if f.ResourceType != "" {
		clauses = append(clauses, `cloud_nodes.resource_type = `+param(f.ResourceType))
	}
	if f.Provider != "" {
		clauses = append(clauses, `cloud_nodes.source_system = `+param(f.Provider))
	}
	if f.Environment != "" {
		clauses = append(clauses, `1 = 0`)
	}
	if f.ResourceService != "" {
		clauses = append(clauses, `cloud_nodes.service_kind = `+param(f.ResourceService))
	}
	if f.ResourceCategory != "" {
		clauses = append(clauses, `1 = 0`)
	}
	return clauses
}

// graphOnlyStateWhere renders the state branch predicates over state_nodes.
// Provider matches the bound provider_type; kind and resource_type match the
// state's type key; environment, service, and category filters match no
// TerraformStateResource node.
func graphOnlyStateWhere(f Filter, param func(string) string) []string {
	clauses := []string{`TRUE`}
	if f.Kind != "" {
		clauses = append(clauses, `state_nodes.resource_type = `+param(f.Kind))
	}
	if f.ResourceType != "" {
		clauses = append(clauses, `state_nodes.resource_type = `+param(f.ResourceType))
	}
	if f.Provider != "" {
		p := param(f.Provider)
		clauses = append(clauses, `(state_nodes.provider IS NOT NULL AND state_nodes.provider <> '' AND state_nodes.provider = `+p+`)`)
	}
	if f.Environment != "" || f.ResourceService != "" || f.ResourceCategory != "" {
		clauses = append(clauses, `1 = 0`)
	}
	return clauses
}

// graphOnlyBucketSQL maps an inventory dimension to its facts expression for
// one graph-only label, mirroring the graph bucket expressions.
func graphOnlyBucketSQL(label string, dimension Dimension) string {
	switch dimension {
	case DimensionProvider:
		if label == graphOnlyCloudLabel {
			return `CASE WHEN cloud_nodes.source_system = '' THEN 'unknown' ELSE cloud_nodes.source_system END`
		}
		return `CASE WHEN state_nodes.provider IS NULL OR state_nodes.provider = '' THEN 'unknown' ELSE state_nodes.provider END`
	case DimensionEnvironment, DimensionResourceCategory:
		return `'unknown'`
	case DimensionResourceService:
		if label == graphOnlyCloudLabel {
			return `COALESCE(NULLIF(cloud_nodes.service_kind, ''), 'unknown')`
		}
		return `'unknown'`
	case DimensionLabel:
		return `'` + label + `'`
	default:
		return `'unknown'`
	}
}

// graphOnlyBranch assembles one UNION ALL branch aggregating a single
// graph-only label's fact nodes. The branch reads the label's CTE, which the
// caller declares in the query's WITH clause exactly when at least one
// branch needs it.
func graphOnlyBranch(label string, f Filter, selectList []string, from string, args []any) (string, []any) {
	param := func(value string) string {
		args = append(args, value)
		return `$` + strconv.Itoa(len(args))
	}
	var where []string
	if label == graphOnlyCloudLabel {
		where = graphOnlyCloudWhere(f, param)
	} else {
		where = graphOnlyStateWhere(f, param)
	}
	query := `SELECT ` + strings.Join(selectList, `, `) + `
FROM ` + from + `
WHERE ` + strings.Join(where, "\n  AND ")
	return query, args
}

// graphOnlyCountBranch aggregates one graph-only label into (label, provider,
// environment, count) rows for CountBuckets.
func graphOnlyCountBranch(label string, f Filter, args []any) (string, []any) {
	from := `cloud_nodes`
	if label == graphOnlyStateLabel {
		from = `state_nodes`
	}
	bucket := graphOnlyBucketSQL(label, DimensionProvider)
	branch, args := graphOnlyBranch(label, f, []string{
		`'` + label + `'`,
		bucket,
		`'unknown'`,
		`count(*)`,
	}, from, args)
	return branch + `
GROUP BY 1, 2, 3`, args
}

// graphOnlyDimensionBranch aggregates one graph-only label into (bucket,
// count) rows for DimensionBuckets.
func graphOnlyDimensionBranch(label string, f Filter, dimension Dimension, args []any) (string, []any) {
	from := `cloud_nodes`
	if label == graphOnlyStateLabel {
		from = `state_nodes`
	}
	bucket := graphOnlyBucketSQL(label, dimension)
	branch, args := graphOnlyBranch(label, f, []string{bucket, `count(*)`}, from, args)
	return branch + `
GROUP BY 1`, args
}

// graphOnlyCTEs renders the WITH clause for the requested facts labels: the
// provider-services pre-aggregation plus the cloud CTE when CloudResource is
// requested, the binding-first pre-aggregation plus the state CTE when
// TerraformStateResource is requested. Pre-aggregations precede their
// consumers: WITH members can only reference earlier members.
func graphOnlyCTEs(labels []string) string {
	cloud, state := false, false
	for _, label := range labels {
		if label == graphOnlyCloudLabel {
			cloud = true
		}
		if label == graphOnlyStateLabel {
			state = true
		}
	}
	var ctes []string
	if cloud {
		ctes = append(ctes, graphOnlyProviderServicesCTE, graphOnlyCloudNodesCTE)
	}
	if state {
		ctes = append(ctes, graphOnlyBindingFirstCTE, graphOnlyStateNodesCTE)
	}
	if len(ctes) == 0 {
		return ""
	}
	return "WITH " + strings.Join(ctes, ",\n") + "\n"
}
