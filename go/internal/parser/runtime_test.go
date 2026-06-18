package parser

import "testing"

func TestRuntimeParserLoadsKotlinGrammar(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime()
	parser, err := runtime.Parser("kotlin")
	if err != nil {
		t.Fatalf("Parser(kotlin) error = %v, want nil", err)
	}
	parser.Close()
}
