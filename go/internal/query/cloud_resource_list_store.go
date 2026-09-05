// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CloudResourceListIdentity is the narrow owner-ledger identity used to choose
// one authorized cloud-resource page before hydrating its graph properties.
type CloudResourceListIdentity struct {
	UID          string
	ResourceType string
}

// CloudResourceListPageFilter is the normalized SQL page selection contract.
// Limit is the fetch bound, normally the requested page size plus one.
type CloudResourceListPageFilter struct {
	Provider             string
	ResourceType         string
	Region               string
	AccountID            string
	AfterResourceType    string
	AfterID              string
	Limit                int
	AllScopes            bool
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// CloudResourceListStore selects current, authorized CloudResource identities
// from the graph owner ledger in deterministic keyset order.
type CloudResourceListStore interface {
	ListCloudResourceIdentities(context.Context, CloudResourceListPageFilter) ([]CloudResourceListIdentity, error)
}

// PostgresCloudResourceListStore implements CloudResourceListStore against the
// graph_node_owner ledger and the active source-fact generation tables.
type PostgresCloudResourceListStore struct {
	db *sql.DB
}

// NewPostgresCloudResourceListStore returns the production Postgres page store.
func NewPostgresCloudResourceListStore(db *sql.DB) *PostgresCloudResourceListStore {
	return &PostgresCloudResourceListStore{db: db}
}

// CurrentAuthorizedCloudResourceUIDs returns the subset of candidateUIDs whose
// CloudResource owner-ledger row is backed by an active-generation, non-tombstoned
// source fact the caller is authorized to see. It reuses the exact active-fact +
// authorization predicate ListCloudResourceIdentities applies, but keyed on the
// candidate uid set instead of a keyset page, so the same "current + authorized"
// contract holds. An empty candidate set returns no rows without a query.
func (s *PostgresCloudResourceListStore) CurrentAuthorizedCloudResourceUIDs(
	ctx context.Context,
	candidateUIDs []string,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("cloud resource list database is required")
	}
	if len(candidateUIDs) == 0 {
		return nil, nil
	}
	query, args := buildCloudResourceCurrentInventoryQuery(candidateUIDs, allScopes, allowedRepositoryIDs, allowedScopeIDs)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select current authorized cloud resource uids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	uids := make([]string, 0, len(candidateUIDs))
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan current authorized cloud resource uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current authorized cloud resource uids: %w", err)
	}
	return uids, nil
}

// CurrentAuthorizedCloudResourcesByDigest examines at most
// supplyChainCloudRuntimeProbePerDigestLimit owner-ledger rows PER REQUESTED
// DIGEST, in deterministic digest, ARN, and uid order.
//
// Freshness and caller authorization are checked BEFORE that bound, so the bound
// counts eligible rows: a digest whose first rows are stale, tombstoned, or
// outside the caller's grants still yields the later row that is current and
// authorized. The bound being per digest is what stops one widely-deployed image
// from spending the whole page budget and leaving every other finding with no
// runtime evidence (#5789).
func (s *PostgresCloudResourceListStore) CurrentAuthorizedCloudResourcesByDigest(
	ctx context.Context,
	digests []string,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]CloudResourceRuntimeDigestMatch, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("cloud resource list database is required")
	}
	if len(digests) == 0 {
		return nil, nil
	}
	query, args := buildCloudResourceRuntimeDigestQuery(
		digests,
		allScopes,
		allowedRepositoryIDs,
		allowedScopeIDs,
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select current authorized cloud resources by runtime digest: %w", err)
	}
	defer func() { _ = rows.Close() }()

	matches := make([]CloudResourceRuntimeDigestMatch, 0, supplyChainCloudRuntimeProbeMaxResults)
	for rows.Next() {
		var match CloudResourceRuntimeDigestMatch
		if err := rows.Scan(&match.UID, &match.Digest, &match.ARN); err != nil {
			return nil, fmt.Errorf("scan current authorized cloud resource runtime digest: %w", err)
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current authorized cloud resource runtime digests: %w", err)
	}
	return matches, nil
}

func buildCloudResourceRuntimeDigestQuery(
	digests []string,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (string, []any) {
	args := make([]any, 0, 4)
	bind := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	digestSet := bind(digests)
	authorization := ""
	if !allScopes {
		repositories := bind(allowedRepositoryIDs)
		scopes := bind(allowedScopeIDs)
		authorization = "\n          AND ((scope.scope_kind = 'repository' AND scope.source_key = ANY(" + repositories + "::text[]))" +
			" OR fact.scope_id = ANY(" + scopes + "::text[]))"
	}
	perDigestLimit := bind(supplyChainCloudRuntimeProbePerDigestLimit(len(digests)))

	// The candidate bound is PER DIGEST, applied through a LATERAL so each
	// digest gets its own bounded, ordered index scan. A single global LIMIT
	// over the whole ordered set does not share: measured on a skewed corpus
	// (one digest on 30,000 resources, 20 others on 100 each), the old shape
	// returned 200 rows for exactly ONE digest and zero for the other twenty,
	// so twenty findings silently kept their CI-declared tier. See
	// docs/internal/evidence/5789-per-digest-bound.md.
	//
	// Freshness and authorization run INSIDE the lateral, BEFORE the limit, so
	// the bound counts ELIGIBLE rows. Bounding first and filtering after looks
	// cheaper and is wrong: a digest whose first ten (arn, uid) rows are stale,
	// tombstoned, or outside the caller's grants would return nothing even
	// though a later row is current and authorized, which is a genuinely
	// running vulnerable image reported as not running (codex review).
	//
	// The bound is the shared budget with a floor (see
	// supplyChainCloudRuntimeProbePerDigestLimit), so a single-digest page keeps
	// exactly its previous breadth while a crowded page still guarantees every
	// digest evidence.
	//
	// Still bounded and still deterministic: total work is at most
	// len(digests) x perDigestLimit rows returned, and the ORDER BY inside the
	// lateral is the same (digest, arn, uid) the index is built on, so the row
	// set is reproducible run to run -- a security evidence field must not vary.
	return `
WITH wanted AS (
  SELECT DISTINCT unnest(` + digestSet + `::text[]) AS digest
), candidates AS MATERIALIZED (
  SELECT per_digest.uid,
         per_digest.digest,
         per_digest.arn
  FROM wanted
  CROSS JOIN LATERAL (
    SELECT owner.uid,
           owner.winning_row->>'running_image_digest' AS digest,
           owner.winning_row->>'arn' AS arn
    FROM graph_node_owner AS owner
    WHERE owner.winning_row->>'resource_type' IS NOT NULL
      AND NULLIF(BTRIM(owner.winning_row->>'running_image_digest'), '') IS NOT NULL
      AND owner.winning_row->>'running_image_digest' = wanted.digest
      AND NULLIF(BTRIM(owner.winning_row->>'arn'), '') IS NOT NULL
      AND COALESCE((
            SELECT TRUE
            FROM fact_records AS fact
            JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id
            JOIN scope_generations AS generation ON generation.generation_id = fact.generation_id
            WHERE fact.fact_id = owner.winning_row->>'source_fact_id'
              AND scope.active_generation_id = fact.generation_id
              AND generation.scope_id = scope.scope_id
              AND generation.status = 'active'
              AND fact.is_tombstone = FALSE` + authorization + `
            LIMIT 1
          ), FALSE)
    ORDER BY owner.winning_row->>'running_image_digest', owner.winning_row->>'arn', owner.uid
    LIMIT ` + perDigestLimit + `
  ) AS per_digest
)
SELECT candidate.uid,
       candidate.digest,
       candidate.arn
FROM candidates AS candidate
ORDER BY candidate.digest, candidate.arn, candidate.uid`, args
}

// buildCloudResourceCurrentInventoryQuery builds the candidate-keyed variant of
// buildCloudResourceIdentityListQuery: the same active-generation, non-tombstone,
// and scope-authorization predicate, filtered to owner.uid = ANY($candidates)
// instead of a keyset page. Every value is bound, including grants.
func buildCloudResourceCurrentInventoryQuery(
	candidateUIDs []string,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (string, []any) {
	args := make([]any, 0, 3)
	bind := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	candidates := bind(candidateUIDs)

	authorization := ""
	if !allScopes {
		repositories := bind(allowedRepositoryIDs)
		scopes := bind(allowedScopeIDs)
		authorization = "\n          AND ((scope.scope_kind = 'repository' AND scope.source_key = ANY(" + repositories + "::text[]))" +
			" OR fact.scope_id = ANY(" + scopes + "::text[]))"
	}

	return `
SELECT owner.uid
FROM graph_node_owner AS owner
WHERE owner.uid = ANY(` + candidates + `::text[])
  AND owner.winning_row->>'resource_type' IS NOT NULL
  AND COALESCE((
        SELECT TRUE
        FROM fact_records AS fact
        JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id
        JOIN scope_generations AS generation ON generation.generation_id = fact.generation_id
        WHERE fact.fact_id = owner.winning_row->>'source_fact_id'
          AND scope.active_generation_id = fact.generation_id
          AND generation.scope_id = scope.scope_id
          AND generation.status = 'active'
          AND fact.is_tombstone = FALSE` + authorization + `
        LIMIT 1
      ), FALSE)`, args
}

// ListCloudResourceIdentities returns at most filter.Limit identities. The
// correlated active-fact probe is deliberately bounded by LIMIT 1: it keeps
// Postgres driving the ordered owner-ledger index and proves authorization for
// each candidate before applying the outer page bound.
func (s *PostgresCloudResourceListStore) ListCloudResourceIdentities(
	ctx context.Context,
	filter CloudResourceListPageFilter,
) ([]CloudResourceListIdentity, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("cloud resource list database is required")
	}
	query, args := buildCloudResourceIdentityListQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select cloud resource identity page: %w", err)
	}
	defer func() { _ = rows.Close() }()

	identities := make([]CloudResourceListIdentity, 0, filter.Limit)
	for rows.Next() {
		var identity CloudResourceListIdentity
		if err := rows.Scan(&identity.UID, &identity.ResourceType); err != nil {
			return nil, fmt.Errorf("scan cloud resource identity page: %w", err)
		}
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud resource identity page: %w", err)
	}
	return identities, nil
}

// buildCloudResourceIdentityListQuery builds all 32 production combinations
// of provider, resource type, region, account, and keyset cursor predicates.
// Every value is bound, including grants and the outer LIMIT.
func buildCloudResourceIdentityListQuery(filter CloudResourceListPageFilter) (string, []any) {
	args := make([]any, 0, 9)
	bind := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	authorization := ""
	if !filter.AllScopes {
		repositories := bind(filter.AllowedRepositoryIDs)
		scopes := bind(filter.AllowedScopeIDs)
		authorization = "\n          AND ((scope.scope_kind = 'repository' AND scope.source_key = ANY(" + repositories + "::text[]))" +
			" OR fact.scope_id = ANY(" + scopes + "::text[]))"
	}

	conditions := []string{
		"owner.winning_row->>'resource_type' IS NOT NULL",
		`COALESCE((
        SELECT TRUE
        FROM fact_records AS fact
        JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id
        JOIN scope_generations AS generation ON generation.generation_id = fact.generation_id
        WHERE fact.fact_id = owner.winning_row->>'source_fact_id'
          AND scope.active_generation_id = fact.generation_id
          AND generation.scope_id = scope.scope_id
          AND generation.status = 'active'
          AND fact.is_tombstone = FALSE` + authorization + `
        LIMIT 1
      ), FALSE)`,
	}
	if filter.Provider != "" {
		conditions = append(conditions, "owner.winning_row->>'collector_kind' = "+bind(filter.Provider))
	}
	if filter.ResourceType != "" {
		conditions = append(conditions, "owner.winning_row->>'resource_type' = "+bind(filter.ResourceType))
	}
	if filter.Region != "" {
		conditions = append(conditions, "owner.winning_row->>'region' = "+bind(filter.Region))
	}
	if filter.AccountID != "" {
		conditions = append(conditions, "owner.winning_row->>'account_id' = "+bind(filter.AccountID))
	}
	if filter.AfterID != "" {
		afterType := bind(filter.AfterResourceType)
		afterID := bind(filter.AfterID)
		conditions = append(conditions,
			"(owner.winning_row->>'resource_type', owner.uid) > ("+afterType+", "+afterID+")")
	}
	limit := bind(filter.Limit)

	return `
SELECT owner.uid, owner.winning_row->>'resource_type' AS resource_type
FROM graph_node_owner AS owner
WHERE ` + strings.Join(conditions, "\n  AND ") + `
ORDER BY owner.winning_row->>'resource_type', owner.uid
LIMIT ` + limit, args
}
