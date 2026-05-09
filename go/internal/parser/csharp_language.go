package parser

import (
	"slices"

	csharpparser "github.com/eshu-hq/eshu/go/internal/parser/csharp"
)

func (e *Engine) parseCSharp(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("c_sharp")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return csharpparser.Parse(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanCSharp(path string) ([]string, error) {
	parser, err := e.runtime.Parser("c_sharp")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := csharpparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
