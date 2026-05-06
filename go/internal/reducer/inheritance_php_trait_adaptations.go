package reducer

import "strings"

type inheritanceTraitAlias struct {
	TraitName        string
	SourceMethodName string
	AliasMethodName  string
}

func inheritancePayloadTraitAdaptations(payload map[string]any) []string {
	return semanticPayloadMetadataStringSlice(payload, "trait_adaptations")
}

func inheritanceTraitOverrideTargets(adaptation string) []string {
	trimmed := strings.TrimSpace(adaptation)
	if trimmed == "" {
		return nil
	}

	lower := strings.ToLower(trimmed)
	key := " insteadof "
	index := strings.Index(lower, key)
	if index < 0 {
		return nil
	}

	tail := strings.TrimSpace(trimmed[index+len(key):])
	if tail == "" {
		return nil
	}

	parts := strings.Split(tail, ",")
	targets := make([]string, 0, len(parts))
	for _, part := range parts {
		if target := inheritanceTraitName(part); target != "" {
			targets = append(targets, target)
		}
	}
	return dedupeNonEmptyStrings(targets)
}

func inheritanceTraitAliasTargets(adaptation string) []string {
	alias, ok := inheritanceTraitAliasMapping(adaptation)
	if !ok {
		return nil
	}

	return []string{alias.TraitName}
}

func inheritanceTraitAliasMapping(adaptation string) (inheritanceTraitAlias, bool) {
	trimmed := strings.TrimSpace(adaptation)
	if trimmed == "" {
		return inheritanceTraitAlias{}, false
	}

	lower := strings.ToLower(trimmed)
	key := " as "
	index := strings.Index(lower, key)
	if index < 0 {
		return inheritanceTraitAlias{}, false
	}

	head := strings.TrimSpace(trimmed[:index])
	if head == "" {
		return inheritanceTraitAlias{}, false
	}

	parts := strings.Split(head, "::")
	if len(parts) != 2 {
		return inheritanceTraitAlias{}, false
	}

	traitName := inheritanceTraitName(parts[0])
	sourceMethodName := strings.TrimSpace(parts[1])
	if traitName == "" || sourceMethodName == "" {
		return inheritanceTraitAlias{}, false
	}

	tail := strings.TrimSpace(trimmed[index+len(key):])
	if tail == "" {
		return inheritanceTraitAlias{}, false
	}

	tailFields := strings.Fields(tail)
	if len(tailFields) == 0 {
		return inheritanceTraitAlias{}, false
	}
	aliasMethodName := strings.TrimSpace(tailFields[len(tailFields)-1])
	if aliasMethodName == "" {
		return inheritanceTraitAlias{}, false
	}

	return inheritanceTraitAlias{
		TraitName:        traitName,
		SourceMethodName: sourceMethodName,
		AliasMethodName:  aliasMethodName,
	}, true
}

func inheritanceTraitName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if index := strings.LastIndex(trimmed, `\`); index >= 0 {
		trimmed = trimmed[index+1:]
	}

	return strings.TrimSpace(trimmed)
}
