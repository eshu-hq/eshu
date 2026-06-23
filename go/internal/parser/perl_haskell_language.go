package parser

import (
	haskellparser "github.com/eshu-hq/eshu/go/internal/parser/haskell"
	perlparser "github.com/eshu-hq/eshu/go/internal/parser/perl"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parsePerl(path string, isDependency bool, options Options) (map[string]any, error) {
	return perlparser.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}

func (e *Engine) preScanPerl(path string) ([]string, error) {
	return perlparser.PreScan(path)
}

func (e *Engine) parseHaskell(path string, isDependency bool, options Options) (map[string]any, error) {
	parser, err := e.runtime.Parser("haskell")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("haskell", parser)

	return haskellparser.ParseWithParser(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	}, parser)
}

func (e *Engine) preScanHaskell(path string) ([]string, error) {
	parser, err := e.runtime.Parser("haskell")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("haskell", parser)

	return haskellparser.PreScanWithParser(path, parser)
}
