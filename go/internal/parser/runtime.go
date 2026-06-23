package parser

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	tree_sitter_dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
	tree_sitter_perl "github.com/alexaandru/go-sitter-forest/perl"
	tree_sitter_sql "github.com/alexaandru/go-sitter-forest/sql"
	tree_sitter_groovy "github.com/dekobon/tree-sitter-groovy/bindings/go"
	tree_sitter_swift "github.com/indigo-net/Brf.it/pkg/parser/treesitter/grammars/swift"
	tree_sitter_kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c_sharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_elixir "github.com/tree-sitter/tree-sitter-elixir/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_haskell "github.com/tree-sitter/tree-sitter-haskell/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type languageLoader func() unsafe.Pointer

// Runtime owns cached tree-sitter language handles and per-language parser
// pools. Pooling eliminates the per-file CGO allocation cost of
// tree_sitter.NewParser + SetLanguage for large repositories.
type Runtime struct {
	mu        sync.Mutex
	languages map[string]*tree_sitter.Language
	// pools is keyed by canonical language name, matching the languages map.
	// Each entry is created lazily on the first Parser call for that language.
	// Once created, pool Get/Put are lock-free (sync.Pool is goroutine-safe).
	pools map[string]*sync.Pool
}

// NewRuntime constructs one native tree-sitter runtime.
func NewRuntime() *Runtime {
	return &Runtime{
		languages: make(map[string]*tree_sitter.Language),
		pools:     make(map[string]*sync.Pool),
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

// Parser borrows a tree-sitter parser configured for name from the per-language
// pool. The caller must return the parser via PutParser after each use, not
// call parser.Close() directly. The parser is guaranteed to be in a reset state.
func (r *Runtime) Parser(name string) (*tree_sitter.Parser, error) {
	canonical, err := normalizeLanguageName(name)
	if err != nil {
		return nil, err
	}

	language, err := r.Language(canonical)
	if err != nil {
		return nil, err
	}

	pool := r.poolFor(canonical, language)

	if p, ok := pool.Get().(*tree_sitter.Parser); ok && p != nil {
		// Reset clears internal cancellation and timeout state so the borrowed
		// parser behaves identically to a freshly allocated one.
		p.Reset()
		return p, nil
	}

	// Pool was empty: allocate a new parser and configure it.
	p := tree_sitter.NewParser()
	if err := p.SetLanguage(language); err != nil {
		p.Close()
		return nil, fmt.Errorf("set parser language %q: %w", name, err)
	}
	return p, nil
}

// PutParser returns a borrowed parser to the per-language pool after resetting
// it. The canonical name must be the same language name used to borrow the
// parser via Parser. The caller must not use p after calling PutParser.
// Passing a nil parser is a no-op. If the language pool does not exist (e.g.
// an unknown language was passed) the parser is closed and freed instead.
func (r *Runtime) PutParser(canonical string, p *tree_sitter.Parser) {
	if p == nil {
		return
	}
	r.mu.Lock()
	pool := r.pools[canonical]
	r.mu.Unlock()
	if pool == nil {
		p.Close()
		return
	}
	p.Reset()
	pool.Put(p)
}

// poolFor returns the sync.Pool for canonical, creating it under r.mu if
// needed. language is captured in the pool's New closure so the pool can
// allocate parsers without re-looking up the language handle.
func (r *Runtime) poolFor(canonical string, language *tree_sitter.Language) *sync.Pool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pool := r.pools[canonical]; pool != nil {
		return pool
	}
	pool := &sync.Pool{
		New: func() any {
			p := tree_sitter.NewParser()
			if err := p.SetLanguage(language); err != nil {
				p.Close()
				return nil
			}
			return p
		},
	}
	r.pools[canonical] = pool
	return pool
}

var builtinLanguageLoaders = map[string]languageLoader{
	"c":          tree_sitter_c.Language,
	"c_sharp":    tree_sitter_c_sharp.Language,
	"cpp":        tree_sitter_cpp.Language,
	"dart":       tree_sitter_dart.Language,
	"elixir":     tree_sitter_elixir.Language,
	"go":         tree_sitter_go.Language,
	"groovy":     tree_sitter_groovy.Language,
	"haskell":    tree_sitter_haskell.Language,
	"java":       tree_sitter_java.Language,
	"javascript": tree_sitter_javascript.Language,
	"kotlin":     tree_sitter_kotlin.Language,
	"perl":       tree_sitter_perl.GetLanguage,
	"php":        tree_sitter_php.LanguagePHP,
	"python":     tree_sitter_python.Language,
	"ruby":       tree_sitter_ruby.Language,
	"rust":       tree_sitter_rust.Language,
	"scala":      tree_sitter_scala.Language,
	"sql":        tree_sitter_sql.GetLanguage,
	"swift":      tree_sitter_swift.Language,
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
	case "dart":
		return "dart", nil
	case "elixir", "ex", "exs":
		return "elixir", nil
	case "go":
		return "go", nil
	case "groovy", "gvy", "gy", "gradle", "jenkinsfile":
		return "groovy", nil
	case "haskell", "hs":
		return "haskell", nil
	case "java":
		return "java", nil
	case "javascript", "js":
		return "javascript", nil
	case "kotlin", "kt", "kts":
		return "kotlin", nil
	case "perl", "pl", "pm":
		return "perl", nil
	case "php":
		return "php", nil
	case "py", "python":
		return "python", nil
	case "rb", "ruby":
		return "ruby", nil
	case "rs", "rust":
		return "rust", nil
	case "scala":
		return "scala", nil
	case "sql":
		return "sql", nil
	case "swift":
		return "swift", nil
	case "tsx":
		return "tsx", nil
	case "ts", "typescript":
		return "typescript", nil
	default:
		return "", fmt.Errorf("unsupported language %q", name)
	}
}
