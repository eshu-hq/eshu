package parser

import (
	"slices"

	javaparser "github.com/eshu-hq/eshu/go/internal/parser/java"
)

func (e *Engine) parseJava(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("java")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return javaparser.Parse(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanJava(path string) ([]string, error) {
	parser, err := e.runtime.Parser("java")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := javaparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
