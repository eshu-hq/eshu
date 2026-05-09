package parser

import (
	"slices"

	swiftparser "github.com/eshu-hq/eshu/go/internal/parser/swift"
)

func (e *Engine) parseSwift(path string, isDependency bool, options Options) (map[string]any, error) {
	return swiftparser.Parse(path, isDependency, sharedOptions(options))
}

func (e *Engine) preScanSwift(path string) ([]string, error) {
	names, err := swiftparser.PreScan(path)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
