// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import "strings"

// Branch shapes.
const (
	// ShapeKeyProbe probes one payload key over growth kinds on a
	// key-leading index.
	ShapeKeyProbe = "key_probe"
	// ShapeScope probes the scopes resolved from a repository list.
	ShapeScope = "scope"
	// ShapeFullFilter evaluates every applicable arm over one bounded kind.
	ShapeFullFilter = "full_filter"
	// ShapeGuard evaluates the retired predicate over foreign-key rows.
	ShapeGuard = "guard"
)

// Branch is one id-resolution statement. Each returns (fact_id, fact_kind)
// for active, non-tombstone rows that satisfy one part of the retired
// predicate; the union over every branch the loader issues is exactly the
// retired row set (ruling 1.2-1.4, pinned by the live differential).
type Branch struct {
	// Name is the closed telemetry label.
	Name  string
	Shape string
	Kinds []string
	// ProbeKey is the payload key a key-probe or file repo_id branch
	// compares; empty for other shapes.
	ProbeKey string
	// Anchor is the index the statement must plan on.
	Anchor string
	// EquivalentAnchors lists other indexes the statement may plan on
	// instead: for a bounded kind, every index partial on that kind alone
	// (btree, or GIN through a full-index bitmap scan) yields the same set of
	// the kind's rows; for a resolved-scope probe, every index leading on
	// (scope_id, generation_id) bounds both. The planner picks the cheapest.
	EquivalentAnchors []string
	SQL               string
	// Params is the bind order of SQL's placeholders.
	Params []Param
	// RunWhen lists the inputs that gate the statement: the loader issues it
	// only when at least one is non-empty. An arm over empty lists matches
	// nothing, so skipping it is exact.
	RunWhen []Param
	// ReturnsSuppressions marks the one branch that returns
	// vulnerability.suppression rows, ordered by byte order and limited to
	// the cap plus one.
	ReturnsSuppressions bool
}

// anchored renders one branch statement in three steps, each MATERIALIZED
// so the plan cannot depend on the planner's join estimates:
//
//  1. candidate reads fact_records only, through the anchor (kind list,
//     tombstone, the key or scope arm) and every arm filter. No arm reads the
//     joined scope row (see arm.resolvedScope), so no filter needs the join.
//     Inlined into the active-generation join, the planner misestimates that
//     two-column join at one row and loops fact_records_scope_generation_idx
//     over every active scope instead (the #8880 fixture measured 55,109
//     buffers for 80 rows that way).
//  2. active applies the retired active-generation join, verbatim, once per
//     distinct (scope_id, generation_id) pair among the candidates rather
//     than once per candidate row.
//  3. the outer query keeps the candidates whose pair survived.
//
// A candidate passes exactly when the retired join admits it: its scope's
// active generation is its generation and that generation's status is
// active. scope_id is the ingestion_scopes key, so active holds at most one
// row per pair and the join adds no duplicates.
func anchored(filters []string) string {
	return "WITH candidate AS MATERIALIZED (\n    SELECT fact.fact_id, fact.fact_kind, fact.scope_id, fact.generation_id\n" +
		"    FROM fact_records AS fact\n    WHERE " + strings.Join(filters, "\n      AND ") + "\n),\n" + `active AS MATERIALIZED (
    SELECT scope.scope_id, scope.active_generation_id
    FROM (SELECT DISTINCT candidate.scope_id, candidate.generation_id FROM candidate) AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE generation.status = 'active'
)
SELECT fact.fact_id, fact.fact_kind
FROM candidate AS fact
JOIN active AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id`
}

func kindAnchor(kinds []string, filters ...string) []string {
	return append([]string{kindPredicate("fact.fact_kind", kinds), "fact.is_tombstone = FALSE"}, filters...)
}

func keyProbe(name, anchor, key string, param Param, kinds ...string) Branch {
	b := newBinder()
	sql := anchored(kindAnchor(kinds, "fact.payload->>'"+key+"' = ANY("+b.list(param)+")"))
	return Branch{
		Name: name, Shape: ShapeKeyProbe, Kinds: kinds, ProbeKey: key, Anchor: anchor,
		SQL: sql, Params: b.order, RunWhen: []Param{param},
	}
}

func fullFilter(name, anchor, kind string) Branch {
	b := newBinder()
	sql := anchored(kindAnchor([]string{kind}, kindDisjunction(b, kind)))
	branch := Branch{Name: name, Shape: ShapeFullFilter, Kinds: []string{kind}, Anchor: anchor}
	if kind == KindSuppression {
		sql += "\nORDER BY convert_to(fact.fact_id, 'UTF8')\nLIMIT " + b.ref(SuppressionLimit)
		branch.ReturnsSuppressions = true
	}
	branch.SQL, branch.Params, branch.RunWhen = sql, b.order, baseParams(b.order)
	return branch
}

// baseParams drops the derived lists from a bind order: a normalized twin is
// empty exactly when its base list is, and the cap is not an input.
func baseParams(order []Param) []Param {
	var base []Param
	for _, p := range order {
		if p <= FileRepositoryIDs {
			base = append(base, p)
		}
	}
	return base
}

// Branches returns the #8880 branch catalog in issue order. The kind list of
// every key probe equals or is a subset of its anchor's predicate list, or
// the partial index is not provable (TestBranchKindsAreProvableByAnchors).
func Branches() []Branch {
	return append(probeBranches(), guard())
}

// probeBranches is every branch except the guard, whose predicate is derived
// from the keys these branches probe.
func probeBranches() []Branch {
	vulnerability := []string{KindCVE, KindAffectedPackage, KindAffectedProduct, KindEPSSScore, KindKnownExploited}
	return []Branch{
		keyProbe("vulnerability_cve_id", IndexVulnerabilityCVEV2, "cve_id", CVEIDs, vulnerability...),
		keyProbe("vulnerability_advisory_id", IndexVulnerabilityAdvisoryV2, "advisory_id", AdvisoryIDs, vulnerability...),
		keyProbe("affected_package_package_id", IndexAffectedPackagePackageID, "package_id", PackageIDs, KindAffectedPackage),
		keyProbe("affected_package_purl", IndexAffectedPackagePURL, "purl", PURLs, KindAffectedPackage),
		keyProbe("affected_product_criteria", IndexAffectedProductCriteria, "criteria", ProductCriteria, KindAffectedProduct),
		keyProbe("sbom_component_purl", IndexSBOMComponentPURL, "purl", PURLs, KindSBOMComponent),
		keyProbe("sbom_component_cpe", IndexSBOMComponentCPE, "cpe", ProductCriteria, KindSBOMComponent),
		keyProbe("sbom_component_document_id", IndexSBOMComponentDocumentID, "document_id", DocumentIDs, KindSBOMComponent),
		keyProbe("package_id", IndexPackageID, "package_id", PackageIDs, packageIDKinds()...),
		keyProbe("package_version_purl", IndexPackageVersionPURL, "purl", PURLs, KindPackageVersion),
		keyProbe("security_alert_cve_id", IndexSecurityAlertCVEID, "cve_id", CVEIDs, KindSecurityAlert),
		keyProbe("vulnerability_hint_advisory_id", IndexVulnerabilityHintAdvisory, "advisory_id", AdvisoryIDs, KindVulnerabilityHint),
		keyProbe("oci_image_digest", IndexOCIImageDigest, "digest", SubjectDigests, KindOCIImageManifest, KindOCIImageIndex),
		keyProbe("oci_tag_resolved_digest", IndexOCITagResolvedDigest, "resolved_digest", SubjectDigests, KindOCIImageTagObservation),
		keyProbe("oci_referrer_subject_digest", IndexOCIReferrerSubject, "subject_digest", SubjectDigests, KindOCIImageReferrer),
		keyProbe("oci_referrer_referrer_digest", IndexOCIReferrerDigest, "referrer_digest", SubjectDigests, KindOCIImageReferrer),
		keyProbe("sbom_attachment_subject_digest", IndexAttachmentSubject, "subject_digest", SubjectDigests, KindSBOMAttachment),
		keyProbe("sbom_attachment_document_id", IndexAttachmentDocument, "document_id", DocumentIDs, KindSBOMAttachment),
		keyProbe("ci_cd_artifact_digest", IndexCICDArtifact, "artifact_digest", SubjectDigests, KindCICDRunCorrelation),
		keyProbe("ci_cd_image_ref", IndexCICDImageRef, "image_ref", ImageRefs, KindCICDRunCorrelation),
		keyProbe("ci_cd_repository_id", IndexCICDRepository, "repository_id", RepositoryIDs, KindCICDRunCorrelation),
		keyProbe("package_consumption_package_id", IndexConsumptionPackageID, "package_id", PackageIDs, KindPackageConsumption),
		keyProbe("package_consumption_repository_id", IndexConsumptionRepository, "repository_id", RepositoryIDs, KindPackageConsumption),
		keyProbe("repository_payload_scope_id", IndexRepositoryPayloadScopeID, "scope_id", RepositoryIDs, repositoryGrowthKinds()...),
		// fact_records_scope_generation_idx leads on (scope_id, generation_id,
		// fact_kind), so the bound arrays and the kind list are all Index Cond
		// (ruling Addendum B: no dedicated index on measured parity). The
		// keyset index leads on the same pair and a custom plan may pick it.
		withEquivalentAnchors(
			scopeProbe("repository_scope", IndexScopeGeneration, RepositoryScopeIDs, RepositoryGenerationIDs, repositoryGrowthKinds(), ""),
			IndexScopeGenerationKeyset),
		fullFilter("vulnerability_suppression", IndexSuppressionActiveScan, KindSuppression),
		fullFilter("platform_materialization", IndexPlatformActiveScan, KindPlatformMaterialized),
		withEquivalentAnchors(fullFilter("workload_identity", IndexWorkloadIdentityScope, KindWorkloadIdentity),
			IndexWorkloadIdentityWorkload, IndexWorkloadIdentityEntityKeys),
		withEquivalentAnchors(fullFilter("service_catalog_correlation", IndexServiceCatalogEntity, KindServiceCatalog),
			IndexServiceCatalogRepository, IndexServiceCatalogService, IndexServiceCatalogOwner, IndexServiceCatalogWorkload,
			IndexServiceCatalogCandidateRepository),
		fileRepoID(),
		// The JS/TS index carries (scope_id, generation_id) after repo_id and
		// holds only JS/TS file rows, so the resolved-scope probe scans it
		// without a heap fetch per non-JS/TS file (ruling 2.2; the fixture
		// planned it in both plan_cache_mode values).
		scopeProbe("file_scope", IndexJSTSReachabilityRepoFile, FileRepositoryScopeIDs, FileRepositoryGenerationIDs, []string{KindFile}, JSTSLanguagePredicate),
	}
}

func withEquivalentAnchors(branch Branch, anchors ...string) Branch {
	branch.EquivalentAnchors = anchors
	return branch
}

// scopeProbe probes the resolved (scope_id, active_generation_id) pairs. The
// two lists are bound as separate arrays, so the anchor sees their cross
// product; that is safe only because scope_generations.generation_id is a
// primary key owned by exactly one scope, so a fact whose generation is in
// the list belongs to that generation's scope, and the active join still
// re-checks the pair. Do not narrow this to generation_id alone (scope_id is
// the leading index column) or widen it past the resolved pairs.
func scopeProbe(name, anchor string, scopes, generations Param, kinds []string, extra string) Branch {
	b := newBinder()
	filters := []string{"fact.scope_id = ANY(" + b.list(scopes) + ")", "fact.generation_id = ANY(" + b.list(generations) + ")"}
	if extra != "" {
		filters = append(filters, extra)
	}
	return Branch{
		Name: name, Shape: ShapeScope, Kinds: kinds, Anchor: anchor,
		SQL: anchored(kindAnchor(kinds, filters...)), Params: b.order, RunWhen: []Param{scopes},
	}
}

func fileRepoID() Branch {
	b := newBinder()
	sql := anchored(kindAnchor([]string{KindFile}, JSTSLanguagePredicate, "fact.payload->>'repo_id' = ANY("+b.list(FileRepositoryIDs)+")"))
	return Branch{
		Name: "file_repo_id", Shape: ShapeKeyProbe, Kinds: []string{KindFile}, ProbeKey: "repo_id",
		Anchor: IndexJSTSReachabilityRepoFile, SQL: sql, Params: b.order, RunWhen: []Param{FileRepositoryIDs},
	}
}

// guard is the foreign-key guard statement: rows the guard index admits, the
// #5237 file gate on FileRepositoryIDs, and the full retired predicate.
func guard() Branch {
	b := newBinder()
	gate := "(fact.fact_kind <> 'file' OR COALESCE(cardinality(" + b.list(FileRepositoryIDs) + "), 0) > 0)"
	sql := anchored([]string{"fact.is_tombstone = FALSE", GuardPredicate(), gate, retiredDisjunction(b)})
	var kinds []string
	for _, info := range Kinds() {
		if info.Class != ClassBounded {
			kinds = append(kinds, info.Kind)
		}
	}
	return Branch{
		Name: "foreign_key_guard", Shape: ShapeGuard, Kinds: kinds, Anchor: IndexSupplyChainImpactKeyGuard,
		SQL: sql, Params: b.order, RunWhen: baseParams(b.order),
	}
}

// ScopeResolutionSQL resolves the active scopes a repository list names
// through the four scope arms of the retired predicate (fact.scope_id joins
// scope.scope_id, so scope_id covers that arm) and returns each scope's
// active generation. It runs in the same snapshot as the branches, so the
// scope branches probe exactly the active (scope, generation) pairs; their
// outer join still re-checks the pair.
const ScopeResolutionSQL = `SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.active_generation_id IS NOT NULL
  AND (
      scope.scope_id = ANY($1::text[])
      OR scope.source_key = ANY($1::text[])
      OR scope.payload->>'repo_id' = ANY($1::text[])
      OR scope.payload->>'id' = ANY($1::text[])
  )
ORDER BY scope.scope_id`

// PayloadPageSQL fetches one page of resolved facts by primary key, with the
// sixteen columns the loader decodes.
const PayloadPageSQL = `SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, '') AS source_uri,
    COALESCE(fact.source_record_id, '') AS source_record_id,
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
WHERE fact.fact_id = ANY($1::text[])`
