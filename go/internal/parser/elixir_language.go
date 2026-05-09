package parser

import (
	"slices"

	elixirparser "github.com/eshu-hq/eshu/go/internal/parser/elixir"
)

func (e *Engine) parseElixir(path string, isDependency bool, options Options) (map[string]any, error) {
	return elixirparser.Parse(path, isDependency, sharedOptions(options))
}

func (e *Engine) preScanElixir(path string) ([]string, error) {
	names, err := elixirparser.PreScan(path)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
