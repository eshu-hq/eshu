package parser

import (
	"slices"

	elixirparser "github.com/eshu-hq/eshu/go/internal/parser/elixir"
)

func (e *Engine) parseElixir(path string, isDependency bool, options Options) (map[string]any, error) {
	parser, err := e.runtime.Parser("elixir")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return elixirparser.ParseWithParser(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanElixir(path string) ([]string, error) {
	parser, err := e.runtime.Parser("elixir")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := elixirparser.PreScanWithParser(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
