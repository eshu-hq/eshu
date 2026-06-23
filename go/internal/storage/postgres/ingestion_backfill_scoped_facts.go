package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// listOnboardedRepoScopedRelationshipFactRecordsQuery is the content-scoped
// sibling of listLatestRelationshipFactRecordsQuery. It preserves the
// latest-generation-per-scope selection and the content/file/gcp relationship
// fact_kind filter, but additionally narrows to facts whose lowercased payload
// text matches at least one onboarding anchor (a repository alias token or
// Terraform provider suffix, plus unconditional ArgoCD over-select markers).
//
// The predicate lower(fact.payload::text) LIKE ANY($1) is a provable superset of
// the facts the in-memory catalogMatcher would match against the same scoped
// catalog: content/file/gcp payloads store candidate strings verbatim, so every
// alias token a match requires is a substring of the lowercased payload. The
// ArgoCD ApplicationSet path synthesizes candidate tokens from cross-file
// template parameters and normalized path basenames that are not guaranteed to
// appear in the fact's own payload, so ArgoCD-shaped facts are over-selected by
// always-present marker anchors (see backfillRelationshipAnchorTerms).
//
// Unlike the full-corpus query, this one's row count scales with the onboarding
// delta's alias surface rather than the fleet size, so per-commit backfill no
// longer ships and iterates every repository's facts.
const listOnboardedRepoScopedRelationshipFactRecordsQuery = `
WITH latest_generations AS (
    SELECT
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            (
                SELECT generation_id
                FROM scope_generations AS candidate
                WHERE candidate.scope_id = generation.scope_id
                ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
                LIMIT 1
            )
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    GROUP BY generation.scope_id, scope.active_generation_id
)
SELECT
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
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
  AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND lower(fact.payload::text) LIKE ANY($1)
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

// listDeferredScopedRelationshipFactRecordsQuery is the self-exclusion variant
// of listOnboardedRepoScopedRelationshipFactRecordsQuery used exclusively by the
// corpus-wide deferred backfill (issue #3659). It accepts two parameters:
//
//	$1 pq.StringArray — LIKE terms derived from non-repo_id aliases (name,
//	   slug tokens) and the unconditional ArgoCD over-select markers. A fact
//	   matching $1 carries a cross-repo reference that is NOT keyed on its own
//	   repo_id, so loading it can produce evidence and it is always loaded.
//
//	$2 pq.StringArray — raw lowercase repo_id values. The repo_id arm uses
//	   EXISTS + strpos to load a fact only when its payload contains a catalog
//	   repo_id value that is not the row's own repo_id.
//
// Why EXISTS, not blind replace(): replace() corrupts overlap cases where the
// own repo_id is a prefix of another repo's repo_id (for example app vs
// app-config). EXISTS compares whole repo_id values, so the overlapping target
// still matches.
//
// Why not a value-exclusion list (payload->>'repo_id' != ALL($2)): every ACTIVE
// repo's own repo_id is in the catalog, so that predicate would exclude EVERY
// active repo's fact from the repo_id arm — including legitimate cross-repo
// references — also breaking truth-equivalence. The EXISTS test excludes only
// the row's exact self-value while keeping all OTHER repos' repo_id matches.
//
// Correctness invariant (truth-equivalence): a fact in repo A that references
// repo B's repo_id (A ≠ B, including B that contains A as a substring) has B's
// value present and B <> A, so EXISTS is true and the fact is loaded. A fact
// whose only repo_id match is its own (A) has no OTHER catalog repo_id present,
// so EXISTS is false and it is dropped — and the in-memory catalogMatcher would
// have skipped that self-match anyway (entry.RepoID == sourceRepoID), so no
// evidence the full-corpus load would have produced is dropped.
//
// The per-commit scoped query (listOnboardedRepoScopedRelationshipFactRecordsQuery)
// uses a single-parameter LIKE ANY and does not need self-exclusion because its
// anchorCatalog is the onboarding delta (new repos only): a new repo's own
// facts are not in the corpus yet, so its repo_id cannot self-match.
const listDeferredScopedRelationshipFactRecordsQuery = `
WITH latest_generations AS (
    SELECT
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            (
                SELECT generation_id
                FROM scope_generations AS candidate
                WHERE candidate.scope_id = generation.scope_id
                ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
                LIMIT 1
            )
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    GROUP BY generation.scope_id, scope.active_generation_id
)
SELECT
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
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
  AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND (
    lower(fact.payload::text) LIKE ANY($1)
    OR EXISTS (
      SELECT 1
      FROM unnest($2::text[]) AS catalog_repo_id(value)
      WHERE catalog_repo_id.value <> lower(COALESCE(fact.payload->>'repo_id', ''))
        AND lower(fact.payload::text) ~ (
          '(^|[^a-z0-9._-])'
          || regexp_replace(catalog_repo_id.value, '([.^$*+?()\[\]{}|\\-])', '\\\1', 'g')
          || '($|[^a-z0-9._-])'
        )
    )
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

// argoCDOverSelectAnchors are lowercase payload markers that force every
// ArgoCD-shaped content/file fact into the scoped fact load regardless of the
// onboarding catalog's aliases. The ArgoCD ApplicationSet discovery path
// (discoverArgoCDDocumentEvidence) renders candidate repoURLs by substituting
// template parameters harvested from a different config repository's content and
// from normalizePlatformToken'd path basenames; those synthesized tokens are not
// guaranteed to appear verbatim in the ArgoCD fact's own payload, so an
// alias-derived predicate could under-select them. Over-selecting the small set
// of ArgoCD facts keeps the load a provable superset without scanning the fleet.
var argoCDOverSelectAnchors = []string{
	"kind: application",
	"kind: applicationset",
	"argocd_applications",
	"argocd_applicationsets",
	`"artifact_type":"argocd"`,
	`"artifact_type": "argocd"`,
}

// backfillRelationshipAnchorTerms returns the lowercase payload anchors the
// per-commit relationship backfill loads facts for: the alias-derived anchors of
// the newly onboarded repositories' catalog entries
// (relationships.CatalogPayloadAnchors) unioned with the unconditional
// ArgoCD-shaped markers (argoCDOverSelectAnchors).
//
// When the new repositories have no usable aliases the alias-derived set is empty
// and this returns nil. That is the intended short-circuit: with no alias anchor
// no content/file/gcp fact can resolve a new repo as a match target, so loading
// the ArgoCD-shaped facts would only ever discover edges whose target repo is not
// in the new-repo-scoped catalog, which DiscoverEvidence drops anyway. Returning
// nil lets the caller skip the fact load entirely instead of scanning ArgoCD
// facts that cannot contribute new evidence.
func backfillRelationshipAnchorTerms(newRepoCatalog []relationships.CatalogEntry) []string {
	anchors := relationships.CatalogPayloadAnchors(newRepoCatalog)
	if len(anchors) == 0 {
		return nil
	}
	combined := make([]string, 0, len(anchors)+len(argoCDOverSelectAnchors))
	combined = append(combined, anchors...)
	combined = append(combined, argoCDOverSelectAnchors...)
	return combined
}

// loadAnchorScopedRelationshipFacts runs the two-phase anchor-scoped fact load
// used by the per-commit backfill (issue #3570). The corpus-wide deferred
// backfill uses loadDeferredAnchorScopedRelationshipFacts instead (issue #3659),
// which adds the self-repo_id exclusion the deferred pass needs; this function
// is the onboarding-delta variant where a new repo's own facts are not yet in the
// corpus, so its repo_id cannot self-match. anchorCatalog is the catalog whose
// aliases seed the content-anchor predicate (the onboarding delta for the
// per-commit path). configResolveCatalog is the catalog the
// ArgoCD phase-two config-repo resolution matches against; callers pass the full
// refreshed catalog so an ApplicationSet's external git-generator config repo is
// resolvable even when it is not in anchorCatalog.
//
// Phase one loads the latest-generation content/file/gcp_cloud_relationship facts
// whose payload matches an anchor (alias-derived anchors plus the unconditional
// ArgoCD markers). The predicate is a provable superset of the facts
// DiscoverEvidence could match against anchorCatalog, so no evidence is dropped
// relative to a full-corpus load against the same catalog. When anchorCatalog has
// no usable aliases the anchor set is empty and the load short-circuits to nil
// without issuing any query: with no anchor no fact can resolve a catalog target.
//
// Phase two reloads the .yaml/.yml/.json files of the external config
// repositories any loaded ArgoCD ApplicationSet's git generator targets. Those
// files reference the deploy repo only through template parameters
// (team/service/path basename), so neither the alias anchors nor the ArgoCD
// markers select them; without them DiscoverEvidence's content index is
// incomplete and the synthesized deploy edge is dropped. The phase-two load is
// bounded to the resolved config repos, never the whole fleet, and is merged
// (de-duplicated by FactID) into the phase-one facts.
func loadAnchorScopedRelationshipFacts(
	ctx context.Context,
	queryer Queryer,
	anchorCatalog []relationships.CatalogEntry,
	configResolveCatalog []relationships.CatalogEntry,
) ([]facts.Envelope, error) {
	anchors := backfillRelationshipAnchorTerms(anchorCatalog)
	if len(anchors) == 0 {
		return nil, nil
	}

	activeFacts, err := loadOnboardedRepoScopedRelationshipFacts(ctx, queryer, anchors)
	if err != nil {
		return nil, err
	}
	if len(activeFacts) == 0 {
		return nil, nil
	}

	configRefs := relationships.ResolveArgoCDGeneratorConfigRepos(activeFacts, configResolveCatalog)
	if len(configRefs) > 0 {
		configRepoIDs := make([]string, 0, len(configRefs))
		for _, ref := range configRefs {
			configRepoIDs = append(configRepoIDs, ref.ConfigRepoID)
		}
		configFacts, err := loadArgoCDGeneratorConfigFacts(ctx, queryer, configRepoIDs)
		if err != nil {
			return nil, fmt.Errorf("load argocd generator config facts for relationship backfill: %w", err)
		}
		activeFacts = mergeRelationshipFacts(activeFacts, configFacts)
	}

	return activeFacts, nil
}

// loadOnboardedRepoScopedRelationshipFacts loads the latest-generation content,
// file, and gcp_cloud_relationship facts whose payload text matches at least one
// supplied anchor, escaping each anchor as a wrapped LIKE term. It returns nil
// without querying when anchors is empty: no fact can match an empty predicate,
// so the per-commit backfill short-circuits instead of issuing a query that
// would return nothing.
func loadOnboardedRepoScopedRelationshipFacts(
	ctx context.Context,
	queryer Queryer,
	anchors []string,
) ([]facts.Envelope, error) {
	if queryer == nil || len(anchors) == 0 {
		return nil, nil
	}

	likeTerms := buildPayloadAnchorLikeTerms(anchors)
	if len(likeTerms) == 0 {
		return nil, nil
	}

	rows, err := queryer.QueryContext(
		ctx,
		listOnboardedRepoScopedRelationshipFactRecordsQuery,
		pq.StringArray(likeTerms),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return loaded, nil
}

// buildPayloadAnchorLikeTerms lowercases each anchor, escapes the LIKE
// metacharacters (\ % _) so an anchor is matched literally, wraps it in %...%
// for a substring match, and de-duplicates the result. The escape character is
// the SQL-default backslash, matched by the ESCAPE clause Postgres applies to
// LIKE by default. Empty anchors are dropped.
func buildPayloadAnchorLikeTerms(anchors []string) []string {
	seen := make(map[string]struct{}, len(anchors))
	terms := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		anchor = strings.ToLower(strings.TrimSpace(anchor))
		if anchor == "" {
			continue
		}
		term := "%" + escapeLikeLiteral(anchor) + "%"
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}

// escapeLikeLiteral escapes the backslash escape character first, then the LIKE
// wildcards % and _, so the literal anchor text matches itself and cannot be
// turned into an accidental wildcard by anchor content.
func escapeLikeLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	value = strings.ReplaceAll(value, "_", `\_`)
	return value
}

// backfillNonRepoIDAnchorTerms returns the non-repo_id LIKE terms for the
// deferred backfill's $1 parameter in listDeferredScopedRelationshipFactRecordsQuery.
// It derives alias tokens from only the NON-first aliases of each catalog entry
// (name, slug — never the repo_id that sits at Aliases[0]) plus the
// unconditional ArgoCD over-select markers.
//
// Why exclude repo_ids here: CatalogPayloadAnchors over the full catalog emits
// repo_id-derived tokens, and every Git content/file payload stores its own
// repo_id field. That causes every fact to self-match the LIKE ANY predicate —
// the defeat described in issue #3659. The deferred path instead sends repo_id
// tokens separately in $2 with the self-exclusion predicate in $3.
//
// Returns nil when no usable non-repo_id aliases exist. The ArgoCD markers are
// suppressed in that case too: with no alias anchor the only ArgoCD facts that
// could match have no in-catalog config repo, so no evidence would result.
func backfillNonRepoIDAnchorTerms(catalog []relationships.CatalogEntry) []string {
	// Build a stripped catalog with Aliases[0] (the repo_id) removed from each
	// entry so CatalogPayloadAnchors only sees name/slug aliases.
	stripped := make([]relationships.CatalogEntry, 0, len(catalog))
	for _, entry := range catalog {
		if len(entry.Aliases) <= 1 {
			// Only the repo_id alias; no non-repo_id aliases for this entry.
			continue
		}
		stripped = append(stripped, relationships.CatalogEntry{
			RepoID:  entry.RepoID,
			Aliases: entry.Aliases[1:],
		})
	}

	anchors := relationships.CatalogPayloadAnchors(stripped)
	if len(anchors) == 0 {
		return nil
	}
	combined := make([]string, 0, len(anchors)+len(argoCDOverSelectAnchors))
	combined = append(combined, anchors...)
	combined = append(combined, argoCDOverSelectAnchors...)
	return combined
}

// loadDeferredAnchorScopedRelationshipFacts is the deferred-backfill replacement
// for loadAnchorScopedRelationshipFacts (issue #3659). It issues the
// self-exclusion query variant (listDeferredScopedRelationshipFactRecordsQuery)
// with two parameters ($1 non-repo_id LIKE terms, $2 raw repo_id values) so the
// DB excludes facts that only match because their own repo_id appears as an
// anchor, while still loading facts that reference ANOTHER repo's repo_id in
// their content. The repo_id arm is an EXISTS(unnest($2)) self-aware substring
// test, so a target repo_id that merely contains the source repo_id as a
// substring is still matched (no blind replace() corruption).
//
// Phase two (ArgoCD config-repo reload) is identical to the per-commit path.
func loadDeferredAnchorScopedRelationshipFacts(
	ctx context.Context,
	queryer Queryer,
	catalog []relationships.CatalogEntry,
) ([]facts.Envelope, error) {
	nonRepoIDTerms := backfillNonRepoIDAnchorTerms(catalog)
	repoIDValues := relationships.CatalogRepoIDValues(catalog)

	// Short-circuit: if neither arm has anything to match, no fact can produce
	// evidence and we avoid the query entirely.
	if len(nonRepoIDTerms) == 0 && len(repoIDValues) == 0 {
		return nil, nil
	}

	nonRepoIDLike := buildPayloadAnchorLikeTerms(nonRepoIDTerms)
	repoIDRaw := make([]string, 0, len(repoIDValues))
	for _, value := range repoIDValues {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			repoIDRaw = append(repoIDRaw, value)
		}
	}

	// Zero-length arrays are valid: LIKE ANY({}) is false, so an empty arm simply
	// never contributes a match.
	if nonRepoIDLike == nil {
		nonRepoIDLike = []string{}
	}
	if repoIDRaw == nil {
		repoIDRaw = []string{}
	}

	rows, err := queryer.QueryContext(
		ctx,
		listDeferredScopedRelationshipFactRecordsQuery,
		pq.StringArray(nonRepoIDLike),
		pq.StringArray(repoIDRaw),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(loaded) == 0 {
		return nil, nil
	}

	// Phase two: reload ArgoCD config-repo files not captured by the anchor
	// predicate. The config catalog is the full catalog so external config repos
	// not in the anchor set are still resolvable.
	configRefs := relationships.ResolveArgoCDGeneratorConfigRepos(loaded, catalog)
	if len(configRefs) > 0 {
		configRepoIDs := make([]string, 0, len(configRefs))
		for _, ref := range configRefs {
			configRepoIDs = append(configRepoIDs, ref.ConfigRepoID)
		}
		configFacts, err := loadArgoCDGeneratorConfigFacts(ctx, queryer, configRepoIDs)
		if err != nil {
			return nil, fmt.Errorf("load argocd generator config facts for deferred relationship backfill: %w", err)
		}
		loaded = mergeRelationshipFacts(loaded, configFacts)
	}

	return loaded, nil
}
