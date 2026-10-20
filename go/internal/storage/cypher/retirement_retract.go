// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"path"
	"strings"
)

// Retirement graph-retraction statements (Deliverable 2 step 2c, #8659).
//
// The reducer-owned G1 to G4 edge retracts reuse the existing EdgeWriter
// paths, and G5 reuses CanonicalNodeWriter.buildRetractStatements with the
// sentinel generation id below, so every generation matches the `<>`
// predicate. This file holds only what those paths cannot express:
//   - G5f, a repo-anchored File retract for files the G5 anchor (the
//     Repository node, which the path-conflict cleanup may already have
//     deleted) cannot reach;
//   - G5p, a root-prefix Parameter retract (Parameters carry no repo_id);
//   - G6, the Repository demotion (demote, never delete: contract A keeps
//     other repositories' incoming edges until their owners retract them,
//     and the orphan sweep reaps the node after its TTL).
//
// All three run as single statements with the runner's cursor owning the
// retry, never as one atomic group: G5p's NOT term and G6's SET keep them
// outside the group-replay classifier by construction, and G5f stays
// replay-safe so a cursor step whose commit never persisted converges on
// re-execution. Every builder fails closed on empty inputs: an empty root
// prefix would match every Parameter in the graph.
const (
	// retirementFileRetractCypher is G5f. It anchors on the File label by
	// repo_id (file_repo_id index) instead of traversing from the
	// Repository node, so files retract when that node is already absent.
	// A file the live owner also projects carries the live repo id under
	// the File.path uniqueness constraint, so it is never matched. The
	// evidence source travels as a parameter, not a literal: every conjunct
	// then names a bound parameter, which keeps the statement inside the
	// group-replay classifier (a cursor step whose commit never persisted
	// converges on re-execution). The plan is unchanged from the literal
	// form (RANGE seek on repo_id, evidence filter after).
	retirementFileRetractCypher = `MATCH (f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = $evidence_source
DETACH DELETE f`

	// retirementProjectorCanonicalEvidenceSource is the only evidence source
	// G5f retracts. A File carrying any other value belongs to another writer
	// (or to the demoted retirement stub) and survives.
	retirementProjectorCanonicalEvidenceSource = "projector/canonical"

	// retirementParameterRetractCypher is G5p. $root_prefix is the captured
	// graph_repo_root plus '/'; $nested_root_prefixes holds path plus '/'
	// of every other Repository nested under the root, read just before
	// each batch, so a nested checkout keeps its Parameters.
	retirementParameterRetractCypher = `MATCH (p:Parameter)
WHERE p.path STARTS WITH $root_prefix
  AND p.evidence_source = 'projector/canonical'
  AND NOT any(n IN $nested_root_prefixes WHERE p.path STARTS WITH n)
DETACH DELETE p`

	// retirementRepositoryDemoteCypher is G6. It writes only properties in
	// the projector-owned set, adds no delete, and clears path so the
	// path-conflict cleanup never matches the demoted node again. The
	// sentinel generation id detaches the node from liveness reads until
	// the sweep reaps it (or re-admission re-promotes it).
	retirementRepositoryDemoteCypher = `MATCH (r:Repository {id: $repo_id})
WHERE r.evidence_source = 'projector/canonical'
SET r.evidence_source = 'retirement/repository', r.path = null, r.local_path = null,
    r.scope_id = null, r.generation_id = $sentinel_generation_id`
)

// RetirementSentinelGenerationID returns the generation id G6 stamps on the
// demoted Repository node. The `retired:` prefix can never equal a real
// generation, so generation-scoped reads skip the node while the id still
// names the retirement that demoted it.
func RetirementSentinelGenerationID(retirementID string) string {
	return "retired:" + retirementID
}

// BuildRetirementFileRetractStatement builds the G5f File retract for one
// retired repository id.
func BuildRetirementFileRetractStatement(repoID string) (Statement, error) {
	if strings.TrimSpace(repoID) == "" {
		return Statement{}, fmt.Errorf("retirement file retract: empty repo id")
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retirementFileRetractCypher,
		Parameters: map[string]any{
			"repo_id":         repoID,
			"evidence_source": retirementProjectorCanonicalEvidenceSource,
		},
	}, nil
}

// normalizeRetirementNestedPrefixes trims and slash-terminates every
// non-blank nested root. An un-slashed "/r/shared" would make STARTS WITH
// also match the sibling "/r/shared-backup/x.go", sparing a retired
// Parameter invisibly to the census that shares this list: always match
// on the root plus '/'.
func normalizeRetirementNestedPrefixes(nestedRootPrefixes []string) []string {
	nested := make([]string, 0, len(nestedRootPrefixes))
	for _, n := range nestedRootPrefixes {
		if p := strings.TrimSpace(n); p != "" {
			nested = append(nested, strings.TrimSuffix(p, "/")+"/")
		}
	}
	return nested
}

// BuildRetirementParameterRetractStatement builds the G5p Parameter retract
// for one captured repository root. root may carry or omit the trailing
// slash; the statement always matches on root plus '/'.
func BuildRetirementParameterRetractStatement(root string, nestedRootPrefixes []string) (Statement, error) {
	prefix := strings.TrimSpace(root)
	if prefix == "" {
		return Statement{}, fmt.Errorf("retirement parameter retract: empty repository root")
	}
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	nested := normalizeRetirementNestedPrefixes(nestedRootPrefixes)
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retirementParameterRetractCypher,
		Parameters: map[string]any{
			"root_prefix":          prefix,
			"nested_root_prefixes": nested,
		},
	}, nil
}

// retirementWorkloadLabels are the reducer-owned node labels the G5w
// cascade deletes by repo_id anchor. Each has a repo_id index (workload,
// workload-instance, and endpoint leg), so every statement seeks.
var retirementWorkloadLabels = []string{"Workload", "WorkloadInstance", "Endpoint"}

// BuildRetirementWorkloadRetractStatements builds the G5w cascade: one
// repo-anchored DETACH DELETE per reducer-owned workload label. It runs
// with the G5 node retracts (before or after G6; it never touches the
// Repository node): the retired repository's Workload, WorkloadInstance,
// and Endpoint nodes go, and their DEFINES, EXPOSES_ENDPOINT, USES, and
// DEPLOYMENT_SOURCE edges die with them. There is no evidence filter on
// purpose: any node carrying the retired repo_id is residue, whichever
// writer stamped it (repo ids are unique per checkout, so no live owner
// can stamp the retired id).
func BuildRetirementWorkloadRetractStatements(repoID string) ([]Statement, error) {
	if strings.TrimSpace(repoID) == "" {
		return nil, fmt.Errorf("retirement workload retract: empty repo id")
	}
	variables := map[string]string{"Workload": "w", "WorkloadInstance": "i", "Endpoint": "e"}
	statements := make([]Statement, 0, len(retirementWorkloadLabels))
	for _, label := range retirementWorkloadLabels {
		variable, ok := variables[label]
		if !ok {
			return nil, fmt.Errorf("retirement workload retract: no variable for label %q", label)
		}
		statements = append(statements, Statement{
			Operation: OperationCanonicalRetract,
			Cypher: fmt.Sprintf(`MATCH (%s:%s)
WHERE %s.repo_id = $repo_id
DETACH DELETE %s`, variable, label, variable, variable),
			Parameters: map[string]any{
				"repo_id": repoID,
			},
		})
	}
	return statements, nil
}

// BuildRetirementRepositoryDemoteStatement builds the G6 Repository
// demotion for one retired repository id.
func BuildRetirementRepositoryDemoteStatement(repoID, retirementID string) (Statement, error) {
	if strings.TrimSpace(repoID) == "" {
		return Statement{}, fmt.Errorf("retirement repository demote: empty repo id")
	}
	if strings.TrimSpace(retirementID) == "" {
		return Statement{}, fmt.Errorf("retirement repository demote: empty retirement id")
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retirementRepositoryDemoteCypher,
		Parameters: map[string]any{
			"repo_id":                repoID,
			"sentinel_generation_id": RetirementSentinelGenerationID(retirementID),
		},
	}, nil
}

// Retirement root reads. The capture seam writes graph_repo_root once while
// the live rows still exist; these reads cross-check that capture and feed
// the shared-root decision on every resume.
const (
	// retirementRepoRootCypher reads the live Repository path for the
	// capture cross-check.
	retirementRepoRootCypher = `MATCH (r:Repository {id: $repo_id})
RETURN r.path AS path`

	// retirementSharedRootCypher lists the other repositories whose path is
	// the captured root or an ancestor of it. A hit skips G5p (and the
	// Parameter census arm) and records parameter_retract_skipped with the
	// other repository id: Parameters share one key across both ids, so the
	// live owner's Parameters sit under the same root. Ancestry is computed
	// client-side into $ancestors and matched with IN: a literal STARTS
	// WITH <property-expr> predicate over-matches on NornicDB (probe: it
	// returned every Repository but the excluded id), while the equality
	// family is correct on both backends.
	retirementSharedRootCypher = `MATCH (o:Repository)
WHERE o.id <> $repo_id AND o.path IN $ancestors
RETURN o.id AS repo_id, o.path AS path`

	// retirementNestedRootsCypher lists the paths of the other
	// repositories nested under the captured root, for the G5p
	// $nested_root_prefixes parameter.
	retirementNestedRootsCypher = `MATCH (o:Repository)
WHERE o.id <> $repo_id AND o.path IS NOT NULL AND o.path STARTS WITH $root_prefix
RETURN o.path AS path`
)

// RetirementRepoRootQuery returns the capture cross-check read and its
// parameters.
func RetirementRepoRootQuery(repoID string) (string, map[string]any) {
	return retirementRepoRootCypher, map[string]any{"repo_id": repoID}
}

// RetirementSharedRootQuery returns the shared-root read and its
// parameters.
func RetirementSharedRootQuery(repoID, root string) (string, map[string]any) {
	return retirementSharedRootCypher, map[string]any{"repo_id": repoID, "ancestors": retirementAncestorPaths(root)}
}

// retirementAncestorPaths returns root plus its strict ancestor paths,
// root-first. An empty root yields nil, so the IN match fails closed.
// Repository depth is shallow (a handful of segments), so the list stays
// tiny and the match remains an equality seek on both backends.
func retirementAncestorPaths(root string) []string {
	p := strings.TrimSuffix(root, "/")
	if p == "" {
		return nil
	}
	out := []string{p}
	for cur := p; ; {
		parent := path.Dir(cur)
		// Bare "/" is never a repository root shape: appending it would
		// match a filesystem-root repository on every retirement. Stop
		// before it; anything shallower than "/root" has no live shape.
		if parent == cur || parent == "." || parent == "/" {
			break
		}
		out = append(out, parent)
		cur = parent
	}
	return out
}

// RetirementNestedRootsQuery returns the nested-root read and its
// parameters.
func RetirementNestedRootsQuery(repoID, rootPrefix string) (string, map[string]any) {
	return retirementNestedRootsCypher, map[string]any{"repo_id": repoID, "root_prefix": rootPrefix}
}

// RetirementResidueCheck is one anchored arm of the 2z runtime graph
// census. Every arm seeks an index (P10'): the node arms seek their
// label's repo_id index, the Parameter arm seeks the path range index,
// and the edge arm seeks the Repository id constraint. A zero count on
// every arm lets the finalizer run; any residue fails closed.
//
// Entity labels beyond File, Directory, and Function carry repo_id but no
// per-label repo_id index, so no anchored arm can name them without a
// label scan whose db hits scale with the store (P10'). They are retracted
// by the G5 sentinel-generation retract, and the exact unanchored P10 form
// (`MATCH (n) WHERE n.repo_id = $id RETURN labels(n), count(*)`) asserts
// zero across every label in the live acceptance test, not at runtime.
type RetirementResidueCheck struct {
	// Name is the stable arm key the census report and failure_details use.
	Name string
	// Cypher is the arm query. Count arms return one `count` row.
	Cypher string
	// Parameters binds the arm query.
	Parameters map[string]any
}

// BuildRetirementResidueCensus builds the anchored 2z graph census arms for
// one retired repository. includeParameters is false exactly when the
// shared-root check hit: the live repository's Parameters survive the
// skip, so the census must not count them as residue.
func BuildRetirementResidueCensus(
	repoID, root string,
	nestedRootPrefixes []string,
	includeParameters bool,
) ([]RetirementResidueCheck, error) {
	if strings.TrimSpace(repoID) == "" {
		return nil, fmt.Errorf("retirement residue census: empty repo id")
	}
	params := map[string]any{"repo_id": repoID}
	checks := []RetirementResidueCheck{
		{
			Name: "file_residue",
			Cypher: `MATCH (f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical'
RETURN count(f) AS count`,
			Parameters: params,
		},
		{
			Name: "directory_residue",
			Cypher: `MATCH (d:Directory)
WHERE d.repo_id = $repo_id AND d.evidence_source = 'projector/canonical'
RETURN count(d) AS count`,
			Parameters: params,
		},
		{
			Name: "function_residue",
			Cypher: `MATCH (f:Function)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical'
RETURN count(f) AS count`,
			Parameters: params,
		},
		{
			Name: "workload_residue",
			Cypher: `MATCH (w:Workload)
WHERE w.repo_id = $repo_id
RETURN count(w) AS count`,
			Parameters: params,
		},
		{
			Name: "workload_instance_residue",
			Cypher: `MATCH (i:WorkloadInstance)
WHERE i.repo_id = $repo_id
RETURN count(i) AS count`,
			Parameters: params,
		},
		{
			Name: "endpoint_residue",
			Cypher: `MATCH (e:Endpoint)
WHERE e.repo_id = $repo_id
RETURN count(e) AS count`,
			Parameters: params,
		},
		{
			Name: "repository_outgoing_edges",
			Cypher: `MATCH (r:Repository {id: $repo_id})-[rel]->()
RETURN count(rel) AS count`,
			Parameters: params,
		},
	}
	if includeParameters {
		prefix := strings.TrimSpace(root)
		if prefix == "" {
			return nil, fmt.Errorf("retirement residue census: empty repository root")
		}
		prefix = strings.TrimSuffix(prefix, "/") + "/"
		nested := normalizeRetirementNestedPrefixes(nestedRootPrefixes)
		checks = append(checks, RetirementResidueCheck{
			Name: "parameter_residue",
			Cypher: `MATCH (p:Parameter)
WHERE p.path STARTS WITH $root_prefix
  AND p.evidence_source = 'projector/canonical'
  AND NOT any(n IN $nested_root_prefixes WHERE p.path STARTS WITH n)
RETURN count(p) AS count`,
			Parameters: map[string]any{
				"root_prefix":          prefix,
				"nested_root_prefixes": nested,
			},
		})
	}
	return checks, nil
}
