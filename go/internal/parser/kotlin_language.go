package parser

import kotlinparser "github.com/eshu-hq/eshu/go/internal/parser/kotlin"

func (e *Engine) parseKotlin(repoRoot string, path string, isDependency bool, options Options) (map[string]any, error) {
	parser, err := e.runtime.Parser("kotlin")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("kotlin", parser)

	return kotlinparser.Parse(repoRoot, path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanKotlin(repoRoot string, path string) ([]string, error) {
	parser, err := e.runtime.Parser("kotlin")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("kotlin", parser)

	return kotlinparser.PreScan(repoRoot, path, parser)
}
