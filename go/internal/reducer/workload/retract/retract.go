// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retract

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// definesCypher removes a repository's stale DEFINES edges: every DEFINES
// carrying $evidence_source whose Workload is not in the repository's
// keep-list. It anchors on the Repository id uniqueness constraint and expands
// only typed DEFINES edges, so the repository's REPO_CONTAINS fan-out is never
// walked (#7285 shim: NodeUniqueIndexSeek then Expand(All), about 20 db hits
// on a 12,403-file repository). Another writer's DEFINES on the same
// repository, and every edge of another repository, are outside the match.
//
// Every row MUST carry both keep-list keys as lists: a missing key or a null
// value makes `NOT (w.id IN null)` null, and the statement silently deletes
// nothing (probed on Neo4j 2026.08.1). RepositoryEdges always binds an
// explicit, possibly empty, list; an empty list deletes every edge
// $evidence_source wrote for the repository.
const definesCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})-[rel:DEFINES]->(w:Workload)
WHERE rel.evidence_source = $evidence_source
  AND NOT (w.id IN row.keep_workload_ids)
DELETE rel`

// endpointCypher is definesCypher for the repository-side EXPOSES_ENDPOINT
// edge. The workload-side EXPOSES_ENDPOINT edge and the Endpoint node are not
// touched: workload materialization never deletes Endpoint or Workload nodes.
const endpointCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})-[rel:EXPOSES_ENDPOINT]->(e:Endpoint)
WHERE rel.evidence_source = $evidence_source
  AND NOT (e.id IN row.keep_endpoint_ids)
DELETE rel`

// deltaGenerationKey is the repository fact payload flag the git collector
// sets on an incremental generation.
const deltaGenerationKey = "delta_generation"

// KeepList names, for one repository, the Workload and Endpoint ids whose
// DEFINES and repository-side EXPOSES_ENDPOINT edges are current. Every other
// such edge the evidence source wrote for the repository is stale.
type KeepList struct {
	RepoID      string
	WorkloadIDs []string
	EndpointIDs []string
}

// Result reports one stale repository-edge retract. Counted is false when the
// executor chain cannot report relationship delete counters; the deleted
// fields are then zero, not a measured zero. A guarded retract that found
// nothing stale issues no delete and reports Counted with zero deletes.
type Result struct {
	Repositories         int
	DefinesDeleted       int64
	EndpointEdgesDeleted int64
	Counted              bool
	// Mode names how the deletes were chosen: ModeGuarded (read first,
	// delete only stale targets) or one of the unguarded fallbacks that run
	// the keep-list deletes unconditionally.
	Mode string
	// StaleDefines and StaleEndpointEdges count the stale edges the guard
	// read found. They are zero outside ModeGuarded.
	StaleDefines       int
	StaleEndpointEdges int
	// ReadErr is the guard read failure behind ModeUnguardedReadFailed, kept
	// for the log. It never fails the retract.
	ReadErr error
}

// Executor is the graph write port: one parameterized Cypher statement. It is
// the same shape as the reducer's CypherExecutor.
type Executor interface {
	ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error
}

// CountingExecutor is an optional Executor capability: it runs one write
// statement exactly as ExecuteCypher does and also returns the number of
// relationships the committed statement deleted, from the backend's write
// summary. A wrapper whose inner executor cannot count still executes the
// statement and returns ErrUncounted.
type CountingExecutor interface {
	ExecuteCypherCountingRelationshipDeletes(ctx context.Context, cypher string, params map[string]any) (int64, error)
}

// ErrUncounted reports that a counting write succeeded but its executor chain
// could not measure the relationships it deleted.
var ErrUncounted = errors.New("relationship delete count unavailable")

// FullGenerationRepositoryIDs returns the sorted repository graph ids whose
// repository facts in this generation are a full snapshot. A repository with
// any fact flagged delta_generation is excluded: a delta reads only the
// changed files, so a missing candidate there is not evidence that a workload
// is gone. No repository fact means no positive evidence and no retract.
func FullGenerationRepositoryIDs(repositoryFacts []facts.Envelope) []string {
	delta := map[string]bool{}
	for _, env := range repositoryFacts {
		if env.FactKind != "repository" {
			continue
		}
		repoID := strings.TrimSpace(payloadcore.PayloadStr(env.Payload, "graph_id"))
		if repoID == "" {
			continue
		}
		delta[repoID] = delta[repoID] || payloadcore.DeltaPayloadBool(env.Payload, deltaGenerationKey)
	}
	repoIDs := make([]string, 0, len(delta))
	for repoID, isDelta := range delta {
		if !isDelta {
			repoIDs = append(repoIDs, repoID)
		}
	}
	sort.Strings(repoIDs)
	return repoIDs
}

// KeepLists builds one keep-list per repository from the ids the current pass
// committed, keyed by repository id. A repository with no entry keeps nothing.
func KeepLists(repoIDs []string, workloadIDs, endpointIDs map[string][]string) []KeepList {
	keepLists := make([]KeepList, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		keepLists = append(keepLists, KeepList{
			RepoID:      repoID,
			WorkloadIDs: payloadcore.UniqueSortedStrings(workloadIDs[repoID]),
			EndpointIDs: payloadcore.UniqueSortedStrings(endpointIDs[repoID]),
		})
	}
	return keepLists
}

// RepositoryEdges deletes, for each keep-list's repository, the DEFINES and
// repository-side EXPOSES_ENDPOINT edges carrying evidenceSource whose target
// is not kept. Callers MUST pass only repositories whose full generation was
// read (FullGenerationRepositoryIDs), and MUST call it after the current edges
// committed, so a partial read or a failed write never retracts a current
// edge. Both statements are idempotent: a retry deletes nothing new.
//
// With a reader it first reads the repositories' current targets and deletes
// only the stale ones, by id, so a steady-state run issues no DELETE at all
// (NornicDB#296: a zero-row relationship DELETE costs proportional to store
// size). With no reader, or when the read fails, it fails toward deleting:
// the keep-list statements run unconditionally, which is correct and only
// slower. A skipped retract would leave stale edges permanently.
func RepositoryEdges(
	ctx context.Context,
	executor Executor,
	reader Reader,
	batchSize int,
	keepLists []KeepList,
	evidenceSource string,
) (Result, error) {
	result := Result{Counted: true}
	if len(keepLists) == 0 {
		return result, nil
	}
	if executor == nil {
		return result, fmt.Errorf("stale repository edge retract requires an executor")
	}
	if strings.TrimSpace(evidenceSource) == "" {
		return result, fmt.Errorf("stale repository edge retract requires an evidence source")
	}
	if batchSize <= 0 {
		batchSize = len(keepLists)
	}
	for _, keep := range keepLists {
		if strings.TrimSpace(keep.RepoID) == "" {
			return result, fmt.Errorf("stale repository edge retract requires a repository id")
		}
	}
	result.Repositories = len(keepLists)
	if reader == nil {
		result.Mode = ModeUnguardedNoReader
		return keepListRetract(ctx, executor, batchSize, keepLists, evidenceSource, result)
	}
	stale, err := readStaleEdges(ctx, reader, batchSize, keepLists, evidenceSource)
	if err != nil {
		result.Mode, result.ReadErr = ModeUnguardedReadFailed, err
		return keepListRetract(ctx, executor, batchSize, keepLists, evidenceSource, result)
	}
	result.Mode = ModeGuarded
	result.StaleDefines, result.StaleEndpointEdges = len(stale.defines), len(stale.endpoints)
	return deleteStale(ctx, executor, batchSize, stale, evidenceSource, result)
}

// keepListRetract runs the two unguarded keep-list statements over every
// repository.
func keepListRetract(
	ctx context.Context,
	executor Executor,
	batchSize int,
	keepLists []KeepList,
	evidenceSource string,
	result Result,
) (Result, error) {
	rows := make([]map[string]any, 0, len(keepLists))
	for _, keep := range keepLists {
		rows = append(rows, map[string]any{
			"repo_id":           keep.RepoID,
			"keep_workload_ids": payloadcore.NonNilStrings(keep.WorkloadIDs),
			"keep_endpoint_ids": payloadcore.NonNilStrings(keep.EndpointIDs),
		})
	}
	return runRetract(ctx, executor, batchSize, evidenceSource, result,
		retractStatement{cypher: definesCypher, rows: rows},
		retractStatement{cypher: endpointCypher, rows: rows})
}

// retractStatement is one delete template and the rows it runs over.
type retractStatement struct {
	cypher string
	rows   []map[string]any
}

// runRetract runs the DEFINES then the EXPOSES_ENDPOINT statement, skipping
// either one when it has no rows, and records the measured deletes.
func runRetract(
	ctx context.Context,
	executor Executor,
	batchSize int,
	evidenceSource string,
	result Result,
	defines, endpoints retractStatement,
) (Result, error) {
	definesDeleted, definesCounted, err := executeBatches(ctx, executor, batchSize, defines.cypher, defines.rows, evidenceSource)
	if err != nil {
		return result, fmt.Errorf("retract stale workload defines edges: %w", err)
	}
	endpointsDeleted, endpointsCounted, err := executeBatches(ctx, executor, batchSize, endpoints.cypher, endpoints.rows, evidenceSource)
	if err != nil {
		return result, fmt.Errorf("retract stale repository endpoint edges: %w", err)
	}
	result.DefinesDeleted, result.EndpointEdgesDeleted = definesDeleted, endpointsDeleted
	result.Counted = definesCounted && endpointsCounted
	return result, nil
}

// executeBatches runs cypher over rows in batchSize chunks and sums the
// relationship delete counters when the executor can report them. No rows
// issue no statement, which is a measured zero.
func executeBatches(
	ctx context.Context,
	executor Executor,
	batchSize int,
	cypher string,
	rows []map[string]any,
	evidenceSource string,
) (int64, bool, error) {
	if len(rows) == 0 {
		return 0, true, nil
	}
	counter, counted := executor.(CountingExecutor)
	var deleted int64
	for start := 0; start < len(rows); start += batchSize {
		end := min(start+batchSize, len(rows))
		params := map[string]any{"rows": rows[start:end], "evidence_source": evidenceSource}
		if !counted {
			if err := executor.ExecuteCypher(ctx, cypher, params); err != nil {
				return deleted, false, err
			}
			continue
		}
		count, err := counter.ExecuteCypherCountingRelationshipDeletes(ctx, cypher, params)
		if errors.Is(err, ErrUncounted) {
			counted = false
			continue
		}
		if err != nil {
			return deleted, counted, err
		}
		deleted += count
	}
	if !counted {
		deleted = 0
	}
	return deleted, counted, nil
}
