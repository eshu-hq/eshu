package parser

import (
	"regexp"
	"strings"
)

// ColumnLineage describes one output column and the source columns that feed it.
type ColumnLineage struct {
	OutputColumn        string
	SourceColumns       []string
	TransformKind       string
	TransformExpression string
}

// CompiledModelLineage summarizes lineage extracted from one compiled dbt model.
type CompiledModelLineage struct {
	ColumnLineage        []ColumnLineage
	UnresolvedReferences []map[string]string
	ProjectionCount      int
}

type relationBinding struct {
	assetName     string
	columnNames   []string
	columnLineage map[string]ColumnLineage
}

var (
	dbtSelectClauseRe = regexp.MustCompile(`(?is)\bselect\b(?P<select>.*?)\bfrom\b`)
	dbtAsAliasRe      = regexp.MustCompile(`(?is)^(?P<expression>.+?)\s+as\s+(?P<alias>[A-Za-z_][A-Za-z0-9_]*)$`)
	dbtFromRelationRe = regexp.MustCompile(`(?i)\b(?:from|join|left\s+join|right\s+join|inner\s+join|full\s+join|cross\s+join)\s+(?P<relation>[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*){0,2})(?:\s+(?:as\s+)?(?P<alias>[A-Za-z_][A-Za-z0-9_]*))?`)
)

func extractCompiledModelLineage(compiledSQL string, modelName string, relationColumnNames map[string][]string) CompiledModelLineage {
	cteQueries, finalSQL := splitCTEQueries(compiledSQL)
	cteBindings := make(map[string]*relationBinding)
	unresolved := make([]map[string]string, 0)

	for _, cteQuery := range cteQueries {
		cteName := cteQuery[0]
		cteSQL := cteQuery[1]
		cteLineage := extractSelectLineage(cteSQL, modelName, relationBindings(cteSQL, relationColumnNames, cteBindings))
		unresolved = append(unresolved, cteLineage.UnresolvedReferences...)
		cteBindings[cteName] = bindingForColumnLineage(cteLineage.ColumnLineage)
	}

	finalLineage := extractSelectLineage(finalSQL, modelName, relationBindings(finalSQL, relationColumnNames, cteBindings))
	unresolved = append(unresolved, finalLineage.UnresolvedReferences...)
	return CompiledModelLineage{
		ColumnLineage:        finalLineage.ColumnLineage,
		UnresolvedReferences: unresolved,
		ProjectionCount:      finalLineage.ProjectionCount,
	}
}

func bindingForColumnLineage(lineage []ColumnLineage) *relationBinding {
	lineageByName := make(map[string]ColumnLineage)
	columnNames := make([]string, 0, len(lineage))
	for _, item := range lineage {
		lineageByName[item.OutputColumn] = item
		columnNames = append(columnNames, item.OutputColumn)
	}
	return &relationBinding{
		columnNames:   columnNames,
		columnLineage: lineageByName,
	}
}

func relationBindings(sql string, relationColumnNames map[string][]string, cteBindings map[string]*relationBinding) map[string]*relationBinding {
	bindings := make(map[string]*relationBinding)
	for _, match := range dbtFromRelationRe.FindAllStringSubmatch(sql, -1) {
		relationName := strings.TrimSpace(match[1])
		alias := strings.TrimSpace(match[2])
		binding := cteBindings[relationName]
		if binding == nil {
			binding = &relationBinding{
				assetName:     relationName,
				columnNames:   relationColumnNames[relationName],
				columnLineage: map[string]ColumnLineage{},
			}
		}
		names := map[string]struct{}{relationName: {}}
		if alias != "" && !isReservedAlias(alias) {
			names[alias] = struct{}{}
		} else if parts := strings.Split(relationName, "."); len(parts) > 0 {
			names[parts[len(parts)-1]] = struct{}{}
		}
		for name := range names {
			bindings[name] = binding
		}
	}
	return bindings
}

func extractSelectLineage(sql string, modelName string, bindings map[string]*relationBinding) CompiledModelLineage {
	columnLineage := make([]ColumnLineage, 0)
	unresolved := make([]map[string]string, 0)
	selectItems := extractSelectItems(sql)
	for _, selectItem := range selectItems {
		projection := lineageForProjection(selectItem, bindings, modelName)
		columnLineage = append(columnLineage, projection.ColumnLineage...)
		unresolved = append(unresolved, projection.UnresolvedReferences...)
	}
	return CompiledModelLineage{
		ColumnLineage:        columnLineage,
		UnresolvedReferences: unresolved,
		ProjectionCount:      len(selectItems),
	}
}

func splitCTEQueries(compiledSQL string) ([][2]string, string) {
	trimmed := strings.TrimLeft(compiledSQL, " \t\r\n")
	if len(trimmed) < 4 || strings.ToLower(trimmed[:4]) != "with" {
		return nil, compiledSQL
	}

	queries := make([][2]string, 0)
	index := 4
	for index < len(trimmed) {
		for index < len(trimmed) && isSpace(trimmed[index]) {
			index++
		}
		nameStart := index
		for index < len(trimmed) && (isAlphaNumeric(trimmed[index]) || trimmed[index] == '_') {
			index++
		}
		cteName := strings.TrimSpace(trimmed[nameStart:index])
		if cteName == "" {
			break
		}
		for index < len(trimmed) && isSpace(trimmed[index]) {
			index++
		}
		if index < len(trimmed) && trimmed[index] == '(' {
			index = consumeBalancedSegment(trimmed, index)
			for index < len(trimmed) && isSpace(trimmed[index]) {
				index++
			}
		}
		if index+2 > len(trimmed) || strings.ToLower(trimmed[index:index+2]) != "as" {
			break
		}
		index += 2
		for index < len(trimmed) && isSpace(trimmed[index]) {
			index++
		}
		if index >= len(trimmed) || trimmed[index] != '(' {
			break
		}
		queryStart := index + 1
		queryEnd := consumeBalancedSegment(trimmed, index) - 1
		queries = append(queries, [2]string{cteName, trimmed[queryStart:queryEnd]})
		index = queryEnd + 1
		for index < len(trimmed) && isSpace(trimmed[index]) {
			index++
		}
		if index < len(trimmed) && trimmed[index] == ',' {
			index++
			continue
		}
		return queries, trimmed[index:]
	}
	return nil, compiledSQL
}

func consumeBalancedSegment(text string, start int) int {
	depth := 0
	for index := start; index < len(text); index++ {
		switch text[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index + 1
			}
		}
	}
	return len(text)
}

func extractSelectItems(sql string) []string {
	match := dbtSelectClauseRe.FindStringSubmatch(sql)
	if match == nil {
		return nil
	}
	selectClause := match[1]
	items := make([]string, 0)
	current := make([]rune, 0, len(selectClause))
	depth := 0
	inSingleQuote := false
	var prev rune
	for _, character := range selectClause {
		if character == '\'' && prev != '\\' {
			inSingleQuote = !inSingleQuote
		} else if !inSingleQuote {
			switch character {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			case ',':
				if depth == 0 {
					item := strings.TrimSpace(string(current))
					if item != "" {
						items = append(items, item)
					}
					current = current[:0]
					prev = character
					continue
				}
			}
		}
		current = append(current, character)
		prev = character
	}
	if tail := strings.TrimSpace(string(current)); tail != "" {
		items = append(items, tail)
	}
	return items
}

func lineageForProjection(selectItem string, bindings map[string]*relationBinding, modelName string) CompiledModelLineage {
	matches := dbtAsAliasRe.FindStringSubmatch(strings.TrimSpace(selectItem))
	expression := strings.TrimSpace(selectItem)
	outputColumn := ""
	if matches != nil {
		expression = strings.TrimSpace(matches[1])
		outputColumn = strings.TrimSpace(matches[2])
	} else {
		outputColumn = implicitOutputColumn(expression)
	}

	if reason := expressionHonestyGapReason(expression); reason != "" {
		return CompiledModelLineage{UnresolvedReferences: []map[string]string{derivedExpressionGap(expression, modelName, reason)}, ProjectionCount: 1}
	}

	columnLineage := make([]ColumnLineage, 0)
	sourceColumns := make([]string, 0)
	unresolved := make([]map[string]string, 0)
	seenColumns := make(map[string]struct{})
	matchedIdentifiers := expressionIgnoredIdentifiers(expression)
	referenceExpression := referenceScanExpression(expression)

	for _, match := range qualifiedReferenceMatches(referenceExpression) {
		alias := match.Alias
		column := match.Column
		matchedIdentifiers[alias] = struct{}{}
		matchedIdentifiers[column] = struct{}{}
		columns, expanded, unresolvedRef := resolveQualifiedReference(bindings[alias], alias, column, modelName)
		if unresolvedRef != nil {
			unresolved = append(unresolved, unresolvedRef)
			continue
		}
		if len(expanded) > 0 {
			columnLineage = append(columnLineage, expanded...)
			continue
		}
		for _, sourceColumn := range columns {
			if _, ok := seenColumns[sourceColumn]; ok {
				continue
			}
			seenColumns[sourceColumn] = struct{}{}
			sourceColumns = append(sourceColumns, sourceColumn)
		}
	}

	for _, identifier := range unqualifiedIdentifiers(referenceExpression, matchedIdentifiers) {
		columns, unresolvedRef := resolveUnqualifiedReferenceColumns(identifier, bindings, modelName)
		if unresolvedRef != nil {
			unresolved = append(unresolved, unresolvedRef)
			continue
		}
		for _, sourceColumn := range columns {
			if _, ok := seenColumns[sourceColumn]; ok {
				continue
			}
			seenColumns[sourceColumn] = struct{}{}
			sourceColumns = append(sourceColumns, sourceColumn)
		}
	}

	if outputColumn == "" && len(sourceColumns) > 0 {
		outputColumn = sourceColumns[0][strings.LastIndex(sourceColumns[0], ".")+1:]
	}
	if len(sourceColumns) > 0 {
		if reason := expressionPartialReason(expression); reason != "" {
			unresolved = append(unresolved, derivedExpressionGap(expression, modelName, reason))
		}
	}
	if outputColumn != "" && len(sourceColumns) > 0 {
		metadata := transformMetadataForProjection(expression, bindings)
		columnLineage = append(columnLineage, ColumnLineage{
			OutputColumn:        strings.TrimSpace(outputColumn),
			SourceColumns:       sourceColumns,
			TransformKind:       metadata["transform_kind"],
			TransformExpression: metadata["transform_expression"],
		})
	}

	return CompiledModelLineage{
		ColumnLineage:        columnLineage,
		UnresolvedReferences: unresolved,
		ProjectionCount:      1,
	}
}

func referenceScanExpression(expression string) string {
	normalized := stripWrappingParentheses(strings.TrimSpace(expression))
	if inner, ok := unwrapTemplatedExpression(normalized); ok && !dbtBareIdentifierRe.MatchString(inner) && expressionPartialReason(inner) == "" {
		normalized = inner
	}
	matches := dbtQualifiedMacroCallRe.FindStringSubmatch(normalized)
	if matches == nil || (!isSupportedQualifiedMacroExpression(normalized) && !macroExpressionHasLineage(normalized)) {
		return normalized
	}
	return matches[2]
}

func unwrapTemplatedExpression(expression string) (string, bool) {
	normalized := strings.TrimSpace(expression)
	if !strings.HasPrefix(normalized, "{{") || !strings.HasSuffix(normalized, "}}") {
		return "", false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(normalized, "{{"), "}}"))
	if inner == "" {
		return "", false
	}
	return inner, true
}

func resolveQualifiedReference(binding *relationBinding, alias string, column string, modelName string) ([]string, []ColumnLineage, map[string]string) {
	if binding == nil {
		return nil, nil, derivedExpressionGap(alias+"."+column, modelName, "source_alias_not_resolved")
	}
	if column == "*" {
		if len(binding.columnNames) == 0 {
			return nil, nil, derivedExpressionGap(alias+".*", modelName, "wildcard_projection_not_supported")
		}
		expanded := make([]ColumnLineage, 0)
		if binding.assetName != "" {
			for _, expandedColumn := range binding.columnNames {
				expanded = append(expanded, ColumnLineage{
					OutputColumn:  expandedColumn,
					SourceColumns: []string{binding.assetName + "." + expandedColumn},
				})
			}
			return nil, expanded, nil
		}
		for _, expandedColumn := range binding.columnNames {
			item, ok := binding.columnLineage[expandedColumn]
			if !ok {
				continue
			}
			expanded = append(expanded, ColumnLineage{
				OutputColumn:  expandedColumn,
				SourceColumns: item.SourceColumns,
			})
		}
		return nil, expanded, nil
	}
	if binding.assetName != "" {
		return []string{binding.assetName + "." + column}, nil, nil
	}
	item, ok := binding.columnLineage[column]
	if !ok {
		return nil, nil, derivedExpressionGap(alias+"."+column, modelName, "cte_column_not_resolved")
	}
	return item.SourceColumns, nil, nil
}

func resolveUnqualifiedReferenceColumns(identifier string, bindings map[string]*relationBinding, modelName string) ([]string, map[string]string) {
	candidates := make(map[string][]string)
	for _, binding := range bindings {
		columns := bindingColumnsForIdentifier(binding, identifier)
		if len(columns) == 0 {
			continue
		}
		key := strings.Join(columns, "|")
		candidates[key] = columns
	}
	switch len(candidates) {
	case 0:
		return nil, derivedExpressionGap(identifier, modelName, "unqualified_column_reference_not_resolved")
	case 1:
		for _, columns := range candidates {
			return columns, nil
		}
	default:
		return nil, derivedExpressionGap(identifier, modelName, "unqualified_column_reference_ambiguous")
	}
	return nil, nil
}

func bindingColumnsForIdentifier(binding *relationBinding, identifier string) []string {
	if binding == nil {
		return nil
	}
	if binding.assetName != "" {
		for _, columnName := range binding.columnNames {
			if columnName == identifier {
				return []string{binding.assetName + "." + identifier}
			}
		}
		return nil
	}
	item, ok := binding.columnLineage[identifier]
	if !ok {
		return nil
	}
	return item.SourceColumns
}

func implicitOutputColumn(expression string) string {
	matches := dbtQualifiedReferenceScanRe.FindAllStringSubmatch(expression, -1)
	if len(matches) == 1 && matches[0][2] != "*" {
		return matches[0][2]
	}
	return ""
}

func transformMetadataForProjection(expression string, bindings map[string]*relationBinding) map[string]string {
	if metadata := expressionTransformMetadata(expression); metadata != nil {
		return metadata
	}
	return propagatedTransformMetadata(strings.TrimSpace(expression), bindings)
}

func propagatedTransformMetadata(expression string, bindings map[string]*relationBinding) map[string]string {
	normalized := strings.TrimSpace(expression)
	if dbtQualifiedReferenceRe.MatchString(normalized) {
		matches := qualifiedReferenceMatches(normalized)
		if len(matches) == 1 && matches[0].Column != "*" {
			return bindingTransformMetadata(bindings[matches[0].Alias], matches[0].Column)
		}
	}
	if !dbtBareIdentifierRe.MatchString(normalized) {
		return nil
	}
	candidates := make(map[string]map[string]string)
	for _, binding := range bindings {
		metadata := bindingTransformMetadata(binding, normalized)
		if metadata == nil {
			continue
		}
		key := metadata["transform_kind"] + "::" + metadata["transform_expression"]
		candidates[key] = metadata
	}
	if len(candidates) != 1 {
		return nil
	}
	for _, metadata := range candidates {
		return metadata
	}
	return nil
}

func bindingTransformMetadata(binding *relationBinding, columnName string) map[string]string {
	if binding == nil || binding.assetName != "" {
		return nil
	}
	item, ok := binding.columnLineage[columnName]
	if !ok || item.TransformKind == "" || item.TransformExpression == "" {
		return nil
	}
	return map[string]string{
		"transform_kind":       item.TransformKind,
		"transform_expression": item.TransformExpression,
	}
}

func isReservedAlias(alias string) bool {
	switch strings.ToLower(alias) {
	case "on", "where", "group", "order", "limit":
		return true
	default:
		return false
	}
}

func isSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func isAlphaNumeric(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9')
}
