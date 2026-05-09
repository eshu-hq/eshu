package parser

import (
	jsonparser "github.com/eshu-hq/eshu/go/internal/parser/json"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parseJSON(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	return jsonparser.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	}, jsonparser.Config{
		LineageExtractor: jsonLineageExtractor,
	})
}

func jsonLineageExtractor(
	compiledSQL string,
	modelName string,
	relationColumnNames map[string][]string,
) jsonparser.CompiledModelLineage {
	lineage := extractCompiledModelLineage(compiledSQL, modelName, relationColumnNames)
	columnLineage := make([]jsonparser.ColumnLineage, 0, len(lineage.ColumnLineage))
	for _, item := range lineage.ColumnLineage {
		columnLineage = append(columnLineage, jsonparser.ColumnLineage{
			OutputColumn:        item.OutputColumn,
			SourceColumns:       item.SourceColumns,
			TransformKind:       item.TransformKind,
			TransformExpression: item.TransformExpression,
		})
	}
	return jsonparser.CompiledModelLineage{
		ColumnLineage:        columnLineage,
		UnresolvedReferences: lineage.UnresolvedReferences,
		ProjectionCount:      lineage.ProjectionCount,
	}
}
