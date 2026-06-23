package parser

import (
	"slices"

	phpparser "github.com/eshu-hq/eshu/go/internal/parser/php"
)

func (e *Engine) parsePHP(path string, isDependency bool, options Options) (map[string]any, error) {
	parser, err := e.runtime.Parser("php")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("php", parser)

	return phpparser.Parse(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanPHP(path string) ([]string, error) {
	parser, err := e.runtime.Parser("php")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("php", parser)

	names, err := phpparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
