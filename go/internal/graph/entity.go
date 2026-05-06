package graph

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// cypherSafePattern matches identifiers safe for interpolation into Cypher
// labels and property keys.
var cypherSafePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// EntityProps holds the properties for a single graph entity node.
type EntityProps struct {
	// Label is the Neo4j node label (e.g., "Function", "Class").
	Label string

	// FilePath is the absolute path to the file containing the entity.
	FilePath string

	// Name is the entity's short name.
	Name string

	// LineNumber is the 1-based line where the entity is defined.
	LineNumber int

	// UID is the optional canonical unique identifier for uid-based merging.
	UID string

	// Extra holds additional properties to set on the node. Keys must be
	// valid Cypher identifiers.
	Extra map[string]any
}

// ValidateCypherLabel returns an error if the label is not safe for Cypher
// interpolation.
func ValidateCypherLabel(label string) error {
	if !cypherSafePattern.MatchString(label) {
		return fmt.Errorf("invalid Cypher label: %q", label)
	}
	return nil
}

// ValidateCypherPropertyKeys returns an error if any key is not safe for
// Cypher interpolation.
func ValidateCypherPropertyKeys(keys []string) error {
	for _, key := range keys {
		if !cypherSafePattern.MatchString(key) {
			return fmt.Errorf("invalid Cypher property key: %q", key)
		}
	}
	return nil
}

// BuildEntityMergeStatement builds a Cypher MERGE statement for one parsed
// entity. If UID is set the node is merged by uid; otherwise by
// (name, path, line_number). This mirrors the Python
// build_entity_merge_statement in graph/persistence/entities.py.
func BuildEntityMergeStatement(props EntityProps) (CypherStatement, error) {
	if err := ValidateCypherLabel(props.Label); err != nil {
		return CypherStatement{}, err
	}
	if props.FilePath == "" {
		return CypherStatement{}, fmt.Errorf("file_path must not be empty")
	}

	extraKeys := sortedExtraKeys(props.Extra)
	if err := ValidateCypherPropertyKeys(extraKeys); err != nil {
		return CypherStatement{}, err
	}

	params := map[string]any{
		"file_path":   props.FilePath,
		"name":        props.Name,
		"line_number": props.LineNumber,
	}
	for _, key := range extraKeys {
		params[key] = props.Extra[key]
	}

	var identityClause string
	if props.UID != "" {
		identityClause = "uid: $uid"
		params["uid"] = props.UID
	} else {
		identityClause = "name: $name, path: $file_path, line_number: $line_number"
	}

	setParts := []string{
		"n.name = $name",
		"n.path = $file_path",
		"n.line_number = $line_number",
	}
	for _, key := range extraKeys {
		setParts = append(setParts, fmt.Sprintf("n.`%s` = $`%s`", key, key))
	}

	cypher := fmt.Sprintf(`MATCH (f:File {path: $file_path})
MERGE (n:%s {%s})
SET %s
MERGE (f)-[:CONTAINS]->(n)`, props.Label, identityClause, strings.Join(setParts, ", "))

	return CypherStatement{
		Cypher:     cypher,
		Parameters: params,
	}, nil
}

// MergeEntity executes a single entity merge against the graph. This is
// the simple single-entity write path; prefer BatchMergeEntities for bulk
// operations.
func MergeEntity(ctx context.Context, executor CypherExecutor, props EntityProps) error {
	if executor == nil {
		return fmt.Errorf("entity merge executor is required")
	}

	stmt, err := BuildEntityMergeStatement(props)
	if err != nil {
		return fmt.Errorf("build entity merge: %w", err)
	}

	if err := executor.ExecuteCypher(ctx, stmt); err != nil {
		return fmt.Errorf("execute entity merge: %w", err)
	}

	return nil
}

// sortedExtraKeys returns the keys of the extra map in sorted order.
func sortedExtraKeys(extra map[string]any) []string {
	if len(extra) == 0 {
		return nil
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	// Simple insertion sort for small key sets.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
