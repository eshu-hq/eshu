package parser

import (
	rubyparser "github.com/eshu-hq/eshu/go/internal/parser/ruby"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parseRuby(path string, isDependency bool, options Options) (map[string]any, error) {
	return rubyparser.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}

func (e *Engine) preScanRuby(path string) ([]string, error) {
	return rubyparser.PreScan(path)
}
