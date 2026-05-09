package parser

import sqlparser "github.com/eshu-hq/eshu/go/internal/parser/sql"

func (e *Engine) parseSQL(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	return sqlparser.Parse(path, isDependency, sqlparser.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}
