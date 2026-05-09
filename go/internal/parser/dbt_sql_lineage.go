package parser

import "github.com/eshu-hq/eshu/go/internal/parser/dbtsql"

// ColumnLineage describes one output column and the source columns that feed it.
type ColumnLineage = dbtsql.ColumnLineage

// CompiledModelLineage summarizes lineage extracted from one compiled dbt model.
type CompiledModelLineage = dbtsql.CompiledModelLineage

func extractCompiledModelLineage(
	compiledSQL string,
	modelName string,
	relationColumnNames map[string][]string,
) CompiledModelLineage {
	return dbtsql.ExtractCompiledModelLineage(compiledSQL, modelName, relationColumnNames)
}
