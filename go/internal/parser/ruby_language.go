package parser

import (
	"slices"

	rubyparser "github.com/eshu-hq/eshu/go/internal/parser/ruby"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parseRuby(path string, isDependency bool, options Options) (map[string]any, error) {
	parser, err := e.runtime.Parser("ruby")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("ruby", parser)

	return rubyparser.ParseWithParser(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	}, parser)
}

func (e *Engine) preScanRuby(path string) ([]string, error) {
	parser, err := e.runtime.Parser("ruby")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("ruby", parser)

	names, err := rubyparser.PreScanWithParser(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
