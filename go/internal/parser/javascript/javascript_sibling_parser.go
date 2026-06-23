package javascript

import (
	"os"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptSiblingParser parses sibling source files into tree-sitter ASTs for
// the dead-code surface walk. It threads the parent ParserFactory so sibling
// re-exports, imports, and Hapi handler specs are read from the AST instead of
// regex-scanning raw text. Parsed roots are cached per Parse call because the
// surface BFS re-reads the same paths; the cache keeps one walk's repeated reads
// from re-invoking tree-sitter on identical input.
type javaScriptSiblingParser struct {
	factory  ParserFactory
	returner ParserReturner
	cache    map[string]*javaScriptSiblingTree
}

// javaScriptSiblingTree holds a parsed sibling file. A nil root marks a path that
// was missing, empty, unparsable, or of an unsupported extension so the lookup
// is not retried within one Parse call.
type javaScriptSiblingTree struct {
	tree   *tree_sitter.Tree
	root   *tree_sitter.Node
	source []byte
}

// newJavaScriptSiblingParser builds a sibling parser bound to factory and
// returner. A nil factory yields a parser that never resolves a root, mirroring
// the previous behavior when no parser is available.
func newJavaScriptSiblingParser(factory ParserFactory, returner ParserReturner) *javaScriptSiblingParser {
	return &javaScriptSiblingParser{
		factory:  factory,
		returner: returner,
		cache:    make(map[string]*javaScriptSiblingTree),
	}
}

// Close releases every cached tree. It is safe to call on a nil receiver.
func (p *javaScriptSiblingParser) Close() {
	if p == nil {
		return
	}
	for _, entry := range p.cache {
		if entry != nil && entry.tree != nil {
			entry.tree.Close()
		}
	}
	p.cache = make(map[string]*javaScriptSiblingTree)
}

// rootForFile parses path (or returns the cached parse) and yields its root node
// and source. ok is false when the file is missing, empty, of an unsupported
// extension, or fails to parse. Parsing is only attempted for non-empty existing
// files, mirroring the os.ReadFile guards it replaces so absent siblings never
// invoke tree-sitter.
func (p *javaScriptSiblingParser) rootForFile(path string) (*tree_sitter.Node, []byte, bool) {
	if p == nil || p.factory == nil {
		return nil, nil, false
	}
	cleaned := cleanJavaScriptPath(path)
	if cleaned == "" {
		return nil, nil, false
	}
	if entry, ok := p.cache[cleaned]; ok {
		if entry == nil || entry.root == nil {
			return nil, nil, false
		}
		return entry.root, entry.source, true
	}

	root, tree, source := p.parseFile(cleaned)
	if root == nil {
		p.cache[cleaned] = nil
		return nil, nil, false
	}
	entry := &javaScriptSiblingTree{tree: tree, root: root, source: source}
	p.cache[cleaned] = entry
	return entry.root, entry.source, true
}

func (p *javaScriptSiblingParser) parseFile(path string) (*tree_sitter.Node, *tree_sitter.Tree, []byte) {
	runtimeLanguage, ok := javaScriptSiblingRuntimeLanguage(path)
	if !ok {
		return nil, nil, nil
	}
	source, err := os.ReadFile(path)
	if err != nil || len(source) == 0 {
		return nil, nil, nil
	}
	parser, err := p.factory(runtimeLanguage)
	if err != nil || parser == nil {
		return nil, nil, nil
	}
	defer p.returner(runtimeLanguage, parser)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, nil, nil
	}
	root := tree.RootNode()
	if root == nil {
		tree.Close()
		return nil, nil, nil
	}
	return root, tree, source
}

// javaScriptSiblingRuntimeLanguage maps a file extension to the runtime grammar
// name used by the parent parser, returning ok=false for unsupported files.
func javaScriptSiblingRuntimeLanguage(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx":
		return "tsx", true
	case ".ts", ".mts", ".cts":
		return "typescript", true
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript", true
	default:
		return "", false
	}
}
