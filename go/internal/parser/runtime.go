package parser

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c_sharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type languageLoader func() unsafe.Pointer

// Runtime owns cached tree-sitter language handles.
type Runtime struct {
	mu        sync.Mutex
	languages map[string]*tree_sitter.Language
}

// NewRuntime constructs one native tree-sitter runtime.
func NewRuntime() *Runtime {
	return &Runtime{
		languages: make(map[string]*tree_sitter.Language),
	}
}

// Language returns one cached language handle by canonical or alias name.
func (r *Runtime) Language(name string) (*tree_sitter.Language, error) {
	canonical, err := normalizeLanguageName(name)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if language := r.languages[canonical]; language != nil {
		return language, nil
	}

	loader, ok := builtinLanguageLoaders[canonical]
	if !ok {
		return nil, fmt.Errorf("parser language %q is not wired into the Go runtime", canonical)
	}

	language := tree_sitter.NewLanguage(loader())
	r.languages[canonical] = language
	return language, nil
}

// Parser returns a new parser configured for one language.
func (r *Runtime) Parser(name string) (*tree_sitter.Parser, error) {
	language, err := r.Language(name)
	if err != nil {
		return nil, err
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return nil, fmt.Errorf("set parser language %q: %w", name, err)
	}
	return parser, nil
}

var builtinLanguageLoaders = map[string]languageLoader{
	"c":          tree_sitter_c.Language,
	"c_sharp":    tree_sitter_c_sharp.Language,
	"cpp":        tree_sitter_cpp.Language,
	"go":         tree_sitter_go.Language,
	"java":       tree_sitter_java.Language,
	"javascript": tree_sitter_javascript.Language,
	"python":     tree_sitter_python.Language,
	"rust":       tree_sitter_rust.Language,
	"scala":      tree_sitter_scala.Language,
	"tsx":        tree_sitter_typescript.LanguageTSX,
	"typescript": tree_sitter_typescript.LanguageTypescript,
}

func normalizeLanguageName(name string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "c":
		return "c", nil
	case "c#", "c_sharp", "csharp", "cs":
		return "c_sharp", nil
	case "c++", "cpp", "cxx":
		return "cpp", nil
	case "go":
		return "go", nil
	case "java":
		return "java", nil
	case "javascript", "js":
		return "javascript", nil
	case "py", "python":
		return "python", nil
	case "rs", "rust":
		return "rust", nil
	case "scala":
		return "scala", nil
	case "tsx":
		return "tsx", nil
	case "ts", "typescript":
		return "typescript", nil
	default:
		return "", fmt.Errorf("unsupported language %q", name)
	}
}
