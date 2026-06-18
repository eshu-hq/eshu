package parser

import (
	"slices"

	swiftparser "github.com/eshu-hq/eshu/go/internal/parser/swift"
)

func (e *Engine) parseSwift(path string, isDependency bool, options Options) (map[string]any, error) {
	parser, err := e.runtime.Parser("swift")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return swiftparser.Parse(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanSwift(path string) ([]string, error) {
	parser, err := e.runtime.Parser("swift")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := swiftparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
