// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Every query the partition-scoped maintenance pass (#7584) runs against the
// queue or the repository-generation map is DERIVED from the shipped query the
// whole pass runs, never hand-copied. Two derivation shapes are used:
//
//   - Marker insertion: the shipped text up to and including one stable marker
//     line, then one extra conjunct, then the rest of the shipped text. The
//     derived query is the shipped query AND one predicate, so its rows are
//     exactly the shipped rows that satisfy the predicate.
//   - Wrapping: the whole shipped query as a subquery under an outer filter,
//     used where a pre-DISTINCT ON filter would change which row the shipped
//     query picks.
//
// TestTargetedMaintenanceQueriesDeriveFromShippedQueries pins each derived
// string against the shipped text byte for byte, and the live differential
// pins that the derived rows equal the shipped rows filtered in Go. A shipped
// query that loses its marker makes DeriveQueryAtMarker panic at package init.

// activeRepositoryGenerationsRepoMarker is the line of
// backfill.ActiveRepositoryGenerationsQuery after which the repo-bounded read adds its
// conjunct. It filters the outer SELECT DISTINCT ON (repo_id) on repo_id
// itself, so it removes whole repo_id groups and never changes which row the
// DISTINCT ON keeps for a repository that survives.
const activeRepositoryGenerationsRepoMarker = "WHERE repo_id <> ''\n"

// activeRepositoryGenerationsForReposQuery is backfill.ActiveRepositoryGenerationsQuery
// restricted to the repositories in $1. The partition-scoped batch writer reads
// it under the batch's advisory locks instead of re-reading every active
// repository generation in the corpus.
var activeRepositoryGenerationsForReposQuery = DeriveQueryAtMarker(
	backfill.ActiveRepositoryGenerationsQuery,
	activeRepositoryGenerationsRepoMarker,
	"  AND repo_id = ANY($1)\n",
)

// activeRepositoryGenerationsForPartitionsQuery returns the rows of
// backfill.ActiveRepositoryGenerationsQuery whose chosen (scope_id, generation_id) is
// one of the ($1, $2) pairs. It wraps the shipped query rather than inserting a
// scope predicate before DISTINCT ON: when two scopes derive the same repo_id,
// the shipped query keeps one of them, and a pre-DISTINCT scope filter could
// keep the other one instead.
var activeRepositoryGenerationsForPartitionsQuery = "SELECT repo_id, scope_id, generation_id\nFROM (\n" +
	backfill.ActiveRepositoryGenerationsQuery +
	") AS active_repository_generation\n" +
	"WHERE (scope_id, generation_id) IN (SELECT * FROM unnest($1::text[], $2::text[]))\n"

// relationshipReopenPartitionConjunct restricts a relationship-domain listing
// to the exact (scope_id, generation_id) partitions in ($1, $2). The columns
// are qualified to the work alias: the floored listings join
// scope_generations, which carries the same column names.
const relationshipReopenPartitionConjunct = "  AND (work.scope_id, work.generation_id) IN (SELECT * FROM unnest($1::text[], $2::text[]))\n"

// correlationReopenPartitionConjunct restricts a correlation listing, whose $1
// is the domain, to the exact partitions in ($2, $3).
const correlationReopenPartitionConjunct = "  AND (work.scope_id, work.generation_id) IN (SELECT * FROM unnest($2::text[], $3::text[]))\n"

// ShippedQueries carries the exact whole-pass reopen listings owned by the
// parent package. Targeted predicates are derived from these strings.
// DeployableUnit is the changed-evidence listing deployable_unit_correlation
// alone uses (#8283); the other correlation domains use Correlation.
type ShippedQueries struct {
	DeploymentMapping  string
	CodeImportRepoEdge string
	Correlation        string
	DeployableUnit     string
}

// Queries holds the narrowed reopen listings derived once during root
// package initialization, plus their shipped sources for differential proof.
type Queries struct {
	Shipped            ShippedQueries
	DeploymentMapping  string
	CodeImportRepoEdge string
	Correlation        string
	DeployableUnit     string
}

// ProductionQueries is derived once at initialization from the shared
// whole-pass SQL owner. Every pass receives these exact narrowed strings.
var ProductionQueries = DeriveQueries(ShippedQueries{
	DeploymentMapping:  backfill.ListSucceededDeploymentMappingWorkItemsQuery,
	CodeImportRepoEdge: backfill.ListSucceededCodeImportRepoEdgeWorkItemsQuery,
	Correlation:        backfill.ListSucceededReducerWorkItemsByDomainQuery,
	DeployableUnit:     backfill.ListDeployableUnitReopenCandidatesQuery,
})

// DeriveQueries inserts the exact partition conjunct into each shipped
// listing. A missing or repeated marker panics, preserving the old fail-closed
// package-initialization behavior.
func DeriveQueries(shipped ShippedQueries) Queries {
	return Queries{
		Shipped:            shipped,
		DeploymentMapping:  DeriveQueryAtMarker(shipped.DeploymentMapping, CorrelationReopenStageMarker, relationshipReopenPartitionConjunct),
		CodeImportRepoEdge: DeriveQueryAtMarker(shipped.CodeImportRepoEdge, CorrelationReopenStageMarker, relationshipReopenPartitionConjunct),
		Correlation:        DeriveQueryAtMarker(shipped.Correlation, CorrelationReopenStageMarker, correlationReopenPartitionConjunct),
		DeployableUnit:     DeriveQueryAtMarker(shipped.DeployableUnit, CorrelationReopenStageMarker, correlationReopenPartitionConjunct),
	}
}

// CorrelationReopenStageMarker is the shared whole-pass stage predicate.
const CorrelationReopenStageMarker = "WHERE work.stage = 'reducer'\n"

// DeriveQueryAtMarker returns shipped with conjunct inserted directly after the
// single occurrence of marker. It panics when marker is absent or repeated,
// because a derived query that silently fell back to another shape would make
// the partition-scoped pass diverge from the whole pass it must equal.
func DeriveQueryAtMarker(shipped, marker, conjunct string) string {
	if count := strings.Count(shipped, marker); count != 1 {
		panic(fmt.Sprintf("derive partition-scoped maintenance query: marker %q occurs %d times, want 1",
			marker, count))
	}
	index := strings.Index(shipped, marker) + len(marker)
	return shipped[:index] + conjunct + shipped[index:]
}

// loadActiveRepositoryGenerationsForRepos returns the active generation of each
// repository in repoIDs, read through activeRepositoryGenerationsForReposQuery.
// An empty repoIDs returns an empty map without a query.
func loadActiveRepositoryGenerationsForRepos(
	ctx context.Context,
	queryer db.Queryer,
	repoIDs []string,
) (map[string]backfill.RepositoryGenerationIdentity, error) {
	if queryer == nil || len(repoIDs) == 0 {
		return map[string]backfill.RepositoryGenerationIdentity{}, nil
	}
	rows, err := queryer.QueryContext(ctx, activeRepositoryGenerationsForReposQuery, array.StringArray(repoIDs))
	return scanRepositoryGenerationRows(rows, err)
}

// loadActiveRepositoryGenerationsForPartitions returns every repository whose
// active generation, as the shipped corpus-wide read chooses it, is one of
// partitions. Two repositories sharing one scope and generation both appear.
func loadActiveRepositoryGenerationsForPartitions(
	ctx context.Context,
	queryer db.Queryer,
	partitions []backfill.ScopeGenerationPartition,
) (map[string]backfill.RepositoryGenerationIdentity, error) {
	if queryer == nil || len(partitions) == 0 {
		return map[string]backfill.RepositoryGenerationIdentity{}, nil
	}
	scopeIDs, generationIDs := partitionColumns(partitions)
	rows, err := queryer.QueryContext(ctx, activeRepositoryGenerationsForPartitionsQuery,
		array.StringArray(scopeIDs), array.StringArray(generationIDs))
	return scanRepositoryGenerationRows(rows, err)
}

// scanRepositoryGenerationRows scans (repo_id, scope_id, generation_id) rows
// the same way backfill.LoadActiveRepositoryGenerations does.
func scanRepositoryGenerationRows(rows db.Rows, err error) (map[string]backfill.RepositoryGenerationIdentity, error) {
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]backfill.RepositoryGenerationIdentity)
	for rows.Next() {
		var identity backfill.RepositoryGenerationIdentity
		if err := rows.Scan(&identity.RepoID, &identity.ScopeID, &identity.GenerationID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(identity.RepoID) == "" {
			continue
		}
		result[identity.RepoID] = identity
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// listSucceededRelationshipWorkItemsForPartitions runs one partition-filtered
// relationship-domain listing and scans it as reopenWorkItemRef rows.
func listSucceededRelationshipWorkItemsForPartitions(
	ctx context.Context,
	queryer db.Queryer,
	query string,
	domain string,
	partitions []backfill.ScopeGenerationPartition,
) ([]WorkItem, error) {
	if len(partitions) == 0 {
		return nil, nil
	}
	scopeIDs, generationIDs := partitionColumns(partitions)
	rows, err := queryer.QueryContext(ctx, query, array.StringArray(scopeIDs), array.StringArray(generationIDs))
	if err != nil {
		return nil, fmt.Errorf("list succeeded %s work items for partitions: %w", domain, err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]WorkItem, 0)
	for rows.Next() {
		var item WorkItem
		if err := rows.Scan(&item.WorkItemID, &item.Partition.ScopeID, &item.Partition.GenerationID); err != nil {
			return nil, fmt.Errorf("scan succeeded %s work item for partitions: %w", domain, err)
		}
		if strings.TrimSpace(item.WorkItemID) == "" {
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list succeeded %s work items for partitions: %w", domain, err)
	}
	return items, nil
}

// listSucceededReducerWorkItemIDsForDomainAndPartitions runs the
// partition-filtered correlation listing for one domain.
func listSucceededReducerWorkItemIDsForDomainAndPartitions(
	ctx context.Context,
	queryer db.Queryer,
	domain string,
	partitions []backfill.ScopeGenerationPartition,
	query string,
) ([]string, error) {
	if len(partitions) == 0 {
		return nil, nil
	}
	scopeIDs, generationIDs := partitionColumns(partitions)
	rows, err := queryer.QueryContext(ctx, query,
		domain, array.StringArray(scopeIDs), array.StringArray(generationIDs))
	if err != nil {
		return nil, fmt.Errorf("list succeeded %s work items for partitions: %w", domain, err)
	}
	defer func() { _ = rows.Close() }()

	workItemIDs := make([]string, 0)
	for rows.Next() {
		var workItemID string
		if err := rows.Scan(&workItemID); err != nil {
			return nil, fmt.Errorf("scan succeeded %s work item for partitions: %w", domain, err)
		}
		if strings.TrimSpace(workItemID) == "" {
			continue
		}
		workItemIDs = append(workItemIDs, workItemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list succeeded %s work items for partitions: %w", domain, err)
	}
	return workItemIDs, nil
}

// listDeployableUnitReopenCandidatesForPartitions runs the partition-filtered
// changed-evidence listing (#8283) and returns the items to re-open plus the
// count left succeeded because their input did not change.
func listDeployableUnitReopenCandidatesForPartitions(
	ctx context.Context,
	queryer db.Queryer,
	domain string,
	partitions []backfill.ScopeGenerationPartition,
	query string,
) ([]string, int, error) {
	if len(partitions) == 0 {
		return nil, 0, nil
	}
	scopeIDs, generationIDs := partitionColumns(partitions)
	rows, err := queryer.QueryContext(ctx, query,
		domain, array.StringArray(scopeIDs), array.StringArray(generationIDs))
	if err != nil {
		return nil, 0, fmt.Errorf("list %s reopen candidates for partitions: %w", domain, err)
	}
	return backfill.ScanDeployableUnitReopenCandidates(rows)
}

// partitionColumns splits partitions into parallel scope and generation arrays
// for the unnest($1::text[], $2::text[]) predicates above.
func partitionColumns(partitions []backfill.ScopeGenerationPartition) ([]string, []string) {
	scopeIDs := make([]string, 0, len(partitions))
	generationIDs := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		scopeIDs = append(scopeIDs, partition.ScopeID)
		generationIDs = append(generationIDs, partition.GenerationID)
	}
	return scopeIDs, generationIDs
}

// sortedPartitions returns the members of set ordered by (scope_id,
// generation_id), the order every deferred-maintenance fan-out uses.
func sortedPartitions(set map[backfill.ScopeGenerationPartition]struct{}) []backfill.ScopeGenerationPartition {
	partitions := make([]backfill.ScopeGenerationPartition, 0, len(set))
	for partition := range set {
		partitions = append(partitions, partition)
	}
	sort.Slice(partitions, func(i, j int) bool {
		if partitions[i].ScopeID != partitions[j].ScopeID {
			return partitions[i].ScopeID < partitions[j].ScopeID
		}
		return partitions[i].GenerationID < partitions[j].GenerationID
	})
	return partitions
}

// LoadActiveRepositoryGenerationsForRepos reads the shipped active-repository
// result restricted to the requested repository IDs.
func LoadActiveRepositoryGenerationsForRepos(
	ctx context.Context, queryer db.Queryer, repoIDs []string,
) (map[string]backfill.RepositoryGenerationIdentity, error) {
	return loadActiveRepositoryGenerationsForRepos(ctx, queryer, repoIDs)
}

// LoadActiveRepositoryGenerationsForPartitions reads the shipped active-repository
// result restricted to the requested scope-generation partitions.
func LoadActiveRepositoryGenerationsForPartitions(
	ctx context.Context, queryer db.Queryer, partitions []backfill.ScopeGenerationPartition,
) (map[string]backfill.RepositoryGenerationIdentity, error) {
	return loadActiveRepositoryGenerationsForPartitions(ctx, queryer, partitions)
}
