package parser

import (
	"slices"

	rustparser "github.com/eshu-hq/eshu/go/internal/parser/rust"
)

func (e *Engine) parseRust(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("rust")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return rustparser.Parse(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanRust(path string) ([]string, error) {
	parser, err := e.runtime.Parser("rust")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := rustparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
