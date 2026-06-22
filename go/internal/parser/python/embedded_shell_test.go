package python

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

func TestEmbeddedShellCommandsIgnoreCommentsAndStrings(t *testing.T) {
	t.Parallel()

	source := []byte("" +
		"import subprocess\n" +
		"\n" +
		"def documented():\n" +
		"    # subprocess.run(['rm', '-rf', '/tmp/nope'])\n" +
		"    note = \"subprocess.run(['rm', '-rf', '/tmp/nope'])\"\n" +
		"    return note\n" +
		"\n" +
		"def real(cmd):\n" +
		"    return subprocess.run(cmd)\n")

	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	tree := parser.Parse(source, nil)
	t.Cleanup(tree.Close)

	commands := embeddedShellCommands(tree.RootNode(), source)
	if len(commands) != 1 {
		t.Fatalf("embeddedShellCommands() = %#v, want one real call", commands)
	}
	if commands[0].functionName != "real" {
		t.Fatalf("functionName = %q, want real", commands[0].functionName)
	}
	if got, want := commands[0].lineNumber, 9; got != want {
		t.Fatalf("lineNumber = %d, want %d", got, want)
	}
}
