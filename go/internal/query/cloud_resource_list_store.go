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

// CloudResourceCurrentInventoryFilter answers which of a bounded candidate set of
// CloudResource uids are BOTH current — materialized from an active-generation,
// non-tombstoned source fact, so a node left stale by a resource that vanished
// from a later scan is excluded — AND authorized for the caller's scope grants.
// It is the current-inventory + authorization gate the supply-chain runtime probe
// applies to its digest-matched graph nodes, so neither a stale CloudResource node
// nor a cross-scope resource can become runtime_confirmed evidence (#5452 codex
// P1a staleness + #5787 scoped-caller authorization). The candidate set is the
// probe's already-bounded digest matches, so the read stays bounded.
type CloudResourceCurrentInventoryFilter interface {
	CurrentAuthorizedCloudResourceUIDs(
		ctx context.Context,
		candidateUIDs []string,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]string, error)
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
