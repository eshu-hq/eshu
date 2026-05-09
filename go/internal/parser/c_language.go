package parser

import (
	"slices"

	cparser "github.com/eshu-hq/eshu/go/internal/parser/c"
)

func (e *Engine) parseC(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("c")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return cparser.Parse(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanC(path string) ([]string, error) {
	parser, err := e.runtime.Parser("c")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := cparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
