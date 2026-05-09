package parser

import jsparser "github.com/eshu-hq/eshu/go/internal/parser/javascript"

func javaScriptExpressServerSymbols(express map[string]any) []string {
	return jsparser.ExpressServerSymbols(express)
}
