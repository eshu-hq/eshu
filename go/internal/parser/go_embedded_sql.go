package parser

import golangparser "github.com/eshu-hq/eshu/go/internal/parser/golang"

func extractGoEmbeddedSQLQueries(source string) []map[string]any {
	queries := golangparser.EmbeddedSQLQueries(source)
	if len(queries) == 0 {
		return []map[string]any{}
	}
	payload := make([]map[string]any, 0, len(queries))
	for _, query := range queries {
		payload = append(payload, map[string]any{
			"function_name":        query.FunctionName,
			"function_line_number": query.FunctionLineNumber,
			"table_name":           query.TableName,
			"operation":            query.Operation,
			"line_number":          query.LineNumber,
			"api":                  query.API,
		})
	}
	return payload
}
