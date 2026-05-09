package parser

import (
	dartparser "github.com/eshu-hq/eshu/go/internal/parser/dart"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parseDart(path string, isDependency bool, options Options) (map[string]any, error) {
	return dartparser.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}

func (e *Engine) preScanDart(path string) ([]string, error) {
	return dartparser.PreScan(path)
}
