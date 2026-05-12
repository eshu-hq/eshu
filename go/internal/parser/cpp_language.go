package parser

import (
	"slices"

	cppparser "github.com/eshu-hq/eshu/go/internal/parser/cpp"
)

func (e *Engine) parseCPP(
	repoRoot string,
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("cpp")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	payload, err := cppparser.Parse(path, isDependency, sharedOptions(options), parser)
	if err != nil {
		return nil, err
	}
	cppparser.AnnotatePublicHeaderRoots(payload, repoRoot, path)
	return payload, nil
}

func (e *Engine) preScanCPP(path string) ([]string, error) {
	parser, err := e.runtime.Parser("cpp")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := cppparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
