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

// parserFreeListCapacity bounds the number of idle tree-sitter parsers retained
// per language. It must be large enough to cover the parse worker fan-out so
// concurrent parsers reuse instead of reallocating, but small enough that the
// total resident native TSParser count stays bounded at
// parserFreeListCapacity * len(languages). Returns beyond this cap close the
// parser instead of retaining it.
const parserFreeListCapacity = 64

// Runtime owns cached tree-sitter language handles and per-language bounded
// parser free-lists. Reusing parsers eliminates the per-file CGO allocation
// cost of tree_sitter.NewParser + SetLanguage for large repositories.
//
// The free-list is a buffered channel rather than a sync.Pool: a tree-sitter
// Parser wraps a native TSParser C allocation that MUST be released with Close.
// A sync.Pool may silently drop idle entries during GC without notifying the
// owner, which would leak the native allocation in a long-running
// ingester/parser process. The bounded channel instead guarantees every parser
// is either reused on a later borrow or explicitly Closed when the free-list is
// full, so the resident native parser count never grows unbounded.
type Runtime struct {
	mu        sync.Mutex
	languages map[string]*tree_sitter.Language
	// freeLists is keyed by canonical language name, matching the languages
	// map. Each entry is a fixed-capacity buffered channel created lazily on
	// the first Parser call for that language. Channel send/receive is
	// goroutine-safe, so borrow/return need no extra locking once the channel
	// exists.
	freeLists map[string]chan *tree_sitter.Parser
}

// NewRuntime constructs one native tree-sitter runtime.
func NewRuntime() *Runtime {
	return &Runtime{
		languages: make(map[string]*tree_sitter.Language),
		freeLists: make(map[string]chan *tree_sitter.Parser),
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
// bounded free-list. The caller must return the parser via PutParser after each
// use, not call parser.Close() directly. The parser is guaranteed to be in a
// reset state. When the free-list is empty a fresh parser is allocated; the
// caller still returns it via PutParser, which either retains it (if there is
// room under the cap) or Closes it.
func (r *Runtime) Parser(name string) (*tree_sitter.Parser, error) {
	canonical, err := normalizeLanguageName(name)
	if err != nil {
		return nil, err
	}

	language, err := r.Language(canonical)
	if err != nil {
		return nil, err
	}

	freeList := r.freeListFor(canonical)

	select {
	case p := <-freeList:
		// Reset clears internal cancellation and timeout state so the borrowed
		// parser behaves identically to a freshly allocated one.
		p.Reset()
		return p, nil
	default:
		// Free-list empty: allocate a new parser and configure it.
		p := tree_sitter.NewParser()
		if err := p.SetLanguage(language); err != nil {
			p.Close()
			return nil, fmt.Errorf("set parser language %q: %w", name, err)
		}
		return p, nil
	}
}

// PutParser returns a borrowed parser to the per-language bounded free-list
// after resetting it. The canonical name must be the same language name used to
// borrow the parser via Parser. The caller must not use p after calling
// PutParser. Passing a nil parser is a no-op.
//
// If the free-list does not exist (an unknown canonical name) or is already at
// capacity, the parser is Closed and its native TSParser allocation freed
// rather than dropped. This guarantees no parser leaks: every parser is either
// reused or explicitly Closed, and the free-list never grows past its cap.
func (r *Runtime) PutParser(canonical string, p *tree_sitter.Parser) {
	if p == nil {
		return
	}
	r.mu.Lock()
	freeList := r.freeLists[canonical]
	r.mu.Unlock()
	if freeList == nil {
		p.Close()
		return
	}
	p.Reset()
	select {
	case freeList <- p:
		// Retained for reuse.
	default:
		// Free-list full: close to keep the resident parser count bounded.
		p.Close()
	}
}

// freeListFor returns the bounded free-list channel for canonical, creating it
// under r.mu if needed. The channel capacity bounds the idle parsers retained
// for the language.
func (r *Runtime) freeListFor(canonical string) chan *tree_sitter.Parser {
	r.mu.Lock()
	defer r.mu.Unlock()
	if freeList := r.freeLists[canonical]; freeList != nil {
		return freeList
	}
	freeList := make(chan *tree_sitter.Parser, parserFreeListCapacity)
	r.freeLists[canonical] = freeList
	return freeList
}

// freeListLenForTest reports the number of idle parsers currently retained for
// canonical. It exists only for white-box tests that assert the free-list stays
// bounded; production code must not depend on the transient free-list length.
func (r *Runtime) freeListLenForTest(canonical string) int {
	r.mu.Lock()
	freeList := r.freeLists[canonical]
	r.mu.Unlock()
	return len(freeList)
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
