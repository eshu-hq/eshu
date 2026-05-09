package parser

import golangparser "github.com/eshu-hq/eshu/go/internal/parser/golang"

func (e *Engine) parseGo(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return golangparser.Parse(parser, path, isDependency, sharedOptions(options))
}

func (e *Engine) preScanGo(path string) ([]string, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return golangparser.PreScan(parser, path)
}
