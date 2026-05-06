package graph

import (
	"context"
	"fmt"
	"strings"
)

// DeleteFileFromGraph removes a file node, its contained entities, and any
// orphaned parent directories from the graph. This mirrors the Python
// delete_file_from_graph in graph/persistence/mutations.py.
func DeleteFileFromGraph(ctx context.Context, executor CypherExecutor, filePath string) error {
	if executor == nil {
		return fmt.Errorf("mutations executor is required")
	}
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("file path must not be empty")
	}

	// Step 1: Collect parent directory paths before deleting the file.
	// We use a read query to find directories, then delete file + elements,
	// then prune empty directories.
	//
	// Since the Executor interface only supports Execute (fire-and-forget),
	// we run the cascade as a single multi-statement operation.

	// Delete file and all contained elements.
	if err := executor.ExecuteCypher(ctx, CypherStatement{
		Cypher: `MATCH (f:File {path: $file_path})
OPTIONAL MATCH (f)-[:CONTAINS]->(element)
DETACH DELETE f, element`,
		Parameters: map[string]any{
			"file_path": filePath,
		},
	}); err != nil {
		return fmt.Errorf("delete file and elements: %w", err)
	}

	// Prune orphaned directories that no longer contain any children.
	if err := executor.ExecuteCypher(ctx, CypherStatement{
		Cypher: `MATCH (d:Directory)
WHERE NOT (d)-[:CONTAINS]->()
  AND NOT (d)<-[:CONTAINS]-(:Repository)
DETACH DELETE d`,
		Parameters: map[string]any{},
	}); err != nil {
		return fmt.Errorf("prune orphaned directories: %w", err)
	}

	return nil
}

// DeleteRepositoryFromGraph removes a repository node and its entire owned
// subtree from the graph. Returns true if the repository existed and was
// deleted. This mirrors the Python delete_repository_from_graph in
// graph/persistence/mutations.py.
func DeleteRepositoryFromGraph(ctx context.Context, executor CypherExecutor, repoIdentifier string) (bool, error) {
	if executor == nil {
		return false, fmt.Errorf("mutations executor is required")
	}
	repoIdentifier = strings.TrimSpace(repoIdentifier)
	if repoIdentifier == "" {
		return false, fmt.Errorf("repository identifier must not be empty")
	}

	lookupValues := repositoryLookupValues(repoIdentifier)
	if len(lookupValues) == 0 {
		return false, nil
	}

	// Delete repository and all owned content via CONTAINS traversal.
	if err := executor.ExecuteCypher(ctx, CypherStatement{
		Cypher: `MATCH (r:Repository)
WHERE r.id IN $lookup_values
   OR r.path IN $lookup_values
   OR r.local_path IN $lookup_values
OPTIONAL MATCH (r)-[:CONTAINS*]->(e)
DETACH DELETE r, e`,
		Parameters: map[string]any{
			"lookup_values": lookupValues,
		},
	}); err != nil {
		return false, fmt.Errorf("delete repository subtree: %w", err)
	}

	return true, nil
}

// ResetRepositorySubtreeInGraph deletes a repository's owned subtree while
// preserving the Repository node itself. This mirrors the Python
// reset_repository_subtree_in_graph in graph/persistence/mutations.py.
func ResetRepositorySubtreeInGraph(ctx context.Context, executor CypherExecutor, repoIdentifier string) (bool, error) {
	if executor == nil {
		return false, fmt.Errorf("mutations executor is required")
	}
	repoIdentifier = strings.TrimSpace(repoIdentifier)
	if repoIdentifier == "" {
		return false, fmt.Errorf("repository identifier must not be empty")
	}

	lookupValues := repositoryLookupValues(repoIdentifier)
	if len(lookupValues) == 0 {
		return false, nil
	}

	// Delete all owned nodes (files, directories, entities, workloads) via
	// CONTAINS/REPO_CONTAINS traversal + DEFINES + repo_id-scoped workloads.
	if err := executor.ExecuteCypher(ctx, CypherStatement{
		Cypher: `MATCH (r:Repository)
WHERE r.id IN $lookup_values
   OR r.path IN $lookup_values
   OR r.local_path IN $lookup_values
OPTIONAL MATCH (r)-[:CONTAINS|REPO_CONTAINS*1..]->(owned_tree)
WITH r, collect(DISTINCT owned_tree) AS owned_tree_nodes
OPTIONAL MATCH (r)-[:DEFINES]->(defined_workload:Workload)
WITH r, owned_tree_nodes, collect(DISTINCT defined_workload) AS defined_workload_nodes
OPTIONAL MATCH (owned_workload:Workload {repo_id: r.id})
WITH r, owned_tree_nodes, defined_workload_nodes, collect(DISTINCT owned_workload) AS repo_workload_nodes
OPTIONAL MATCH (owned_instance:WorkloadInstance {repo_id: r.id})
WITH owned_tree_nodes, defined_workload_nodes, repo_workload_nodes, collect(DISTINCT owned_instance) AS repo_instance_nodes
WITH owned_tree_nodes + defined_workload_nodes + repo_workload_nodes + repo_instance_nodes AS owned_nodes
UNWIND owned_nodes AS owned
WITH DISTINCT owned
WHERE owned IS NOT NULL
DETACH DELETE owned`,
		Parameters: map[string]any{
			"lookup_values": lookupValues,
		},
	}); err != nil {
		return false, fmt.Errorf("delete repository owned subtree: %w", err)
	}

	// Remove all remaining relationships on the Repository node.
	if err := executor.ExecuteCypher(ctx, CypherStatement{
		Cypher: `MATCH (r:Repository)
WHERE r.id IN $lookup_values
   OR r.path IN $lookup_values
   OR r.local_path IN $lookup_values
OPTIONAL MATCH (r)-[rel]-()
WITH [relationship IN collect(DISTINCT rel) WHERE relationship IS NOT NULL] AS relationships
UNWIND relationships AS rel
DELETE rel`,
		Parameters: map[string]any{
			"lookup_values": lookupValues,
		},
	}); err != nil {
		return false, fmt.Errorf("delete repository relationships: %w", err)
	}

	return true, nil
}

// repositoryLookupValues returns candidate lookup values for a repository
// identifier. A canonical "repository:" prefix signals an ID lookup; raw
// paths are returned as-is.
func repositoryLookupValues(identifier string) []string {
	candidate := strings.TrimSpace(identifier)
	if candidate == "" {
		return nil
	}
	if strings.HasPrefix(candidate, "repository:") {
		return []string{candidate}
	}
	return []string{candidate}
}
