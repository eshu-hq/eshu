package parser

import (
	phpparser "github.com/eshu-hq/eshu/go/internal/parser/php"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parsePHP(path string, isDependency bool, options Options) (map[string]any, error) {
	return phpparser.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}

func (e *Engine) preScanPHP(path string) ([]string, error) {
	return phpparser.PreScan(path)
}
