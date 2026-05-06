package query

import "fmt"

type elixirSemanticEntityType struct {
	baseType      string
	graphLabel    string
	metadataKey   string
	metadataValue string
}

var elixirSemanticEntityTypes = map[string]elixirSemanticEntityType{
	"guard": {
		baseType:      "Function",
		graphLabel:    "Function",
		metadataKey:   "semantic_kind",
		metadataValue: "guard",
	},
	"protocol_implementation": {
		baseType:      "Module",
		graphLabel:    "Module",
		metadataKey:   "module_kind",
		metadataValue: "protocol_implementation",
	},
	"module_attribute": {
		baseType:      "Variable",
		graphLabel:    "Variable",
		metadataKey:   "attribute_kind",
		metadataValue: "module_attribute",
	},
}

func elixirGraphSemanticEntityType(entityType string) (string, string, string, bool) {
	semanticType, ok := elixirSemanticEntityTypes[entityType]
	if !ok || semanticType.graphLabel == "" {
		return "", "", "", false
	}
	return semanticType.graphLabel, semanticType.metadataKey, semanticType.metadataValue, true
}

func contentEntityTypeFilter(entityType string, nextArg int) (string, []any, int) {
	if semanticType, ok := elixirSemanticEntityTypes[entityType]; ok {
		clause := fmt.Sprintf(
			"(entity_type = $%d AND coalesce(metadata ->> '%s', '') = $%d)",
			nextArg,
			semanticType.metadataKey,
			nextArg+1,
		)
		return clause, []any{semanticType.baseType, semanticType.metadataValue}, nextArg + 2
	}
	if entityType == "" {
		return "", nil, nextArg
	}
	return fmt.Sprintf("entity_type = $%d", nextArg), []any{entityType}, nextArg + 1
}
