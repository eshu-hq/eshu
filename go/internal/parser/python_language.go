package parser

import (
	"slices"

	pythonparser "github.com/eshu-hq/eshu/go/internal/parser/python"
)

func (e *Engine) parsePython(
	repoRoot string,
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("python")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return pythonparser.Parse(repoRoot, path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanPython(path string) ([]string, error) {
	parser, err := e.runtime.Parser("python")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := pythonparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
