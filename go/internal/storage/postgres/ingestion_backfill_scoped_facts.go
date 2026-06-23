package postgres

import (
	"context"
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
