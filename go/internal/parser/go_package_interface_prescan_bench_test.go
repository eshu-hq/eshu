package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkPreScanGoPackageSemanticRoots measures the per-repository cost of
// Engine.PreScanGoPackageSemanticRoots over a synthetic Go corpus shaped to
// exercise every per-file extractor pass the engine runs: imported-interface
// parameter methods (consumer view), exported-interface parameter methods
// (producer view), imported direct method call roots, local interface
// imported-method returns, generic constraint interface names, local
// interface methods, and method declaration keys.
//
// The K8s ingest profile (ADR row 1818 follow-up) showed this function
// dominates the parse stage at ~80-90% of CPU when run against 17K Go files.
// Each per-file extractor re-reads source and re-parses the file with
// tree-sitter, so total parse work scales with file_count * extractor_count.
// This benchmark is the focused regression gate that proves the prescan stays
// bounded as new extractor passes are added.
//
// The "Small" subtest uses 30-line files to isolate per-file parse and walk
// fixed overheads. The "K8sShape" subtest uses ~150-line files with many
// call_expressions and methods per file so per-file walk cost dominates over
// per-file parse cost — that matches the K8s corpus shape and gives a more
// honest projection for changes that target walk count rather than parse
// count.
func BenchmarkPreScanGoPackageSemanticRoots(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		repoRoot := b.TempDir()
		paths := writePrescanBenchCorpus(b, repoRoot, 4, 50, prescanBenchShapeSmall)
		engine, err := DefaultEngine()
		if err != nil {
			b.Fatalf("DefaultEngine() error = %v, want nil", err)
		}
		b.ReportMetric(float64(len(paths)), "files")
		for b.Loop() {
			if _, err := engine.PreScanGoPackageSemanticRoots(repoRoot, paths); err != nil {
				b.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
			}
		}
	})

	b.Run("K8sShape", func(b *testing.B) {
		repoRoot := b.TempDir()
		paths := writePrescanBenchCorpus(b, repoRoot, 4, 50, prescanBenchShapeK8s)
		engine, err := DefaultEngine()
		if err != nil {
			b.Fatalf("DefaultEngine() error = %v, want nil", err)
		}
		b.ReportMetric(float64(len(paths)), "files")
		for b.Loop() {
			if _, err := engine.PreScanGoPackageSemanticRoots(repoRoot, paths); err != nil {
				b.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
			}
		}
	})
}

// prescanBenchShape selects between the small per-file payload (fast iteration
// for parse-count-dominated changes) and the K8s-shape payload (large per-file
// payload so walk cost dominates over parse cost).
type prescanBenchShape int

const (
	prescanBenchShapeSmall prescanBenchShape = iota
	prescanBenchShapeK8s
)

// writePrescanBenchCorpus materializes a synthetic Go module with packageCount
// directories of filesPerPackage Go files each, plus a go.mod at the root.
// Each file is shaped to engage every per-file extractor the parent prescan
// invokes; package-level interface declarations and method declarations sit
// alongside callers so chained-call and generic-constraint passes have
// non-trivial work to do. The shape parameter selects between the small
// per-file payload and the K8s-shape payload.
func writePrescanBenchCorpus(b *testing.B, repoRoot string, packageCount, filesPerPackage int, shape prescanBenchShape) []string {
	b.Helper()
	const modulePath = "example.com/prescanbench"
	writeBenchFile(b, filepath.Join(repoRoot, "go.mod"), fmt.Sprintf("module %s\n\ngo 1.22\n", modulePath))

	paths := make([]string, 0, packageCount*filesPerPackage)
	for p := range packageCount {
		packageName := fmt.Sprintf("pkg%d", p)
		packageDir := filepath.Join(repoRoot, packageName)
		if err := os.MkdirAll(packageDir, 0o755); err != nil {
			b.Fatalf("mkdir %s: %v", packageDir, err)
		}
		// One interface-bearing file per package so other files in the package
		// have a same-package interface to satisfy via generic constraints and
		// local interface method returns.
		interfaceFilePath := filepath.Join(packageDir, "iface.go")
		writeBenchFile(b, interfaceFilePath, generatePrescanInterfaceFile(packageName, p))
		paths = append(paths, interfaceFilePath)

		for f := range filesPerPackage {
			filePath := filepath.Join(packageDir, fmt.Sprintf("file%d.go", f))
			var contents string
			switch shape {
			case prescanBenchShapeK8s:
				contents = generatePrescanCallerFileK8sShape(packageName, p, f, modulePath)
			default:
				contents = generatePrescanCallerFile(packageName, p, f, modulePath)
			}
			writeBenchFile(b, filePath, contents)
			paths = append(paths, filePath)
		}
	}
	return paths
}

// generatePrescanInterfaceFile produces an interface-bearing Go file shaped to
// engage LocalInterfaceMethods, LocalInterfaceImportedMethodReturns,
// ExportedInterfaceParamMethods, and MethodDeclarationKeys helpers. The local
// interface returns a fmt.Stringer so downstream
// ImportedDirectMethodCallRootsWithInterfaceReturns has work.
func generatePrescanInterfaceFile(packageName string, idx int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	b.WriteString("import \"fmt\"\n\n")
	fmt.Fprintf(&b, "// Source%d is a package-local interface with one method whose\n", idx)
	b.WriteString("// result type is an imported receiver, exercising the local-interface\n")
	b.WriteString("// imported-method-returns prescan pass.\n")
	fmt.Fprintf(&b, "type Source%d interface {\n", idx)
	b.WriteString("\tStringer() fmt.Stringer\n")
	b.WriteString("\tName() string\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "type Holder%d struct{ value string }\n\n", idx)
	fmt.Fprintf(&b, "func (h *Holder%d) Stringer() fmt.Stringer { return stringerImpl(h.value) }\n", idx)
	fmt.Fprintf(&b, "func (h *Holder%d) Name() string { return h.value }\n\n", idx)
	fmt.Fprintf(&b, "// ExportSource%d keeps the exported-interface parameter prescan on the\n", idx)
	b.WriteString("// same file as the interface declaration it inspects.\n")
	fmt.Fprintf(&b, "func ExportSource%d(src Source%d) []string {\n", idx, idx)
	b.WriteString("\treturn []string{src.Name(), src.Stringer().String()}\n")
	b.WriteString("}\n\n")
	b.WriteString("type stringerImpl string\n\n")
	b.WriteString("func (s stringerImpl) String() string { return string(s) }\n")
	return b.String()
}

// generatePrescanCallerFile produces a Go file shaped to engage the consumer
// interface extractors. The file imports fmt, declares an exported function
// that takes a same-package interface parameter, calls a fmt.Stringer method on
// a returned value (exercising the chained imported direct method call roots),
// and declares a generic helper whose type parameter is constrained by the
// package-local interface (exercising the generic constraint pass).
func generatePrescanCallerFile(packageName string, packageIdx, fileIdx int, modulePath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"strings\"\n")
	b.WriteString(")\n\n")
	// Reference the imports so go vet would not complain; the prescan does not
	// run vet but the shape is realistic.
	fmt.Fprintf(&b, "// Consume%d takes a same-package interface and exercises imported\n", fileIdx)
	b.WriteString("// direct method calls plus cross-file interface consumers.\n")
	fmt.Fprintf(&b, "func Consume%d(src Source%d, prefix string) string {\n", fileIdx, packageIdx)
	b.WriteString("\tout := src.Stringer().String()\n")
	b.WriteString("\tjoined := strings.ToLower(strings.TrimSpace(out))\n")
	b.WriteString("\treturn fmt.Sprintf(\"%s::%s\", prefix, joined)\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// ConsumeGeneric%d uses a generic constraint over the package-local\n", fileIdx)
	b.WriteString("// interface so the generic constraint prescan pass finds matching\n")
	b.WriteString("// method declarations.\n")
	fmt.Fprintf(&b, "func ConsumeGeneric%d[T Source%d](items []T) []string {\n", fileIdx, packageIdx)
	b.WriteString("\tresult := make([]string, 0, len(items))\n")
	b.WriteString("\tfor _, item := range items {\n")
	b.WriteString("\t\tresult = append(result, item.Name())\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn result\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// privateHelper%d adds a method-declaration row so the prescan path\n", fileIdx)
	b.WriteString("// that combines local interface methods with declared methods has\n")
	b.WriteString("// per-file work proportional to file count.\n")
	fmt.Fprintf(&b, "type privateHelper%d struct{ raw string }\n\n", fileIdx)
	fmt.Fprintf(&b, "func (p *privateHelper%d) Name() string { return p.raw }\n", fileIdx)
	fmt.Fprintf(&b, "func (p *privateHelper%d) Stringer() fmt.Stringer { return stringerImpl(p.raw) }\n", fileIdx)
	return b.String()
}

// generatePrescanCallerFileK8sShape produces a ~150-line Go file approximating
// the per-file shape and size of files in the Kubernetes corpus. The file
// includes multiple struct types with methods, several exported functions
// taking interface parameters, dozens of call_expressions inside each
// function, and helper utilities that exercise the parent_lookup,
// import_aliases, and variable_type_index per-file infrastructure. The
// resulting per-file walk cost dominates over per-file parse cost, which
// matches what production K8s ingest measurements show and what the small
// shape under-represents.
func generatePrescanCallerFileK8sShape(packageName string, packageIdx, fileIdx int, modulePath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	b.WriteString("import (\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"strings\"\n")
	b.WriteString("\t\"time\"\n")
	b.WriteString(")\n\n")

	fmt.Fprintf(&b, "// Service%d models a service that depends on Source%d for upstream data.\n", fileIdx, packageIdx)
	fmt.Fprintf(&b, "type Service%d struct {\n", fileIdx)
	b.WriteString("\tname     string\n")
	b.WriteString("\tcreated  time.Time\n")
	b.WriteString("\thandlers map[string]string\n")
	b.WriteString("\tcounts   map[string]int\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// NewService%d constructs a service with default handlers wired up.\n", fileIdx)
	fmt.Fprintf(&b, "func NewService%d(name string) *Service%d {\n", fileIdx, fileIdx)
	fmt.Fprintf(&b, "\treturn &Service%d{\n", fileIdx)
	b.WriteString("\t\tname:     strings.ToLower(strings.TrimSpace(name)),\n")
	b.WriteString("\t\tcreated:  time.Now(),\n")
	b.WriteString("\t\thandlers: make(map[string]string),\n")
	b.WriteString("\t\tcounts:   make(map[string]int),\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// Process%d takes a same-package interface and runs a deep call chain so the\n", fileIdx)
	b.WriteString("// imported direct method call roots prescan has many call_expression nodes\n")
	b.WriteString("// to walk per file.\n")
	fmt.Fprintf(&b, "func (s *Service%d) Process%d(src Source%d, items []string) (string, error) {\n", fileIdx, fileIdx, packageIdx)
	b.WriteString("\tif src == nil {\n")
	b.WriteString("\t\treturn \"\", errors.New(\"nil source\")\n")
	b.WriteString("\t}\n")
	b.WriteString("\tparts := make([]string, 0, len(items))\n")
	b.WriteString("\tfor _, item := range items {\n")
	b.WriteString("\t\ttrimmed := strings.TrimSpace(item)\n")
	b.WriteString("\t\tif trimmed == \"\" {\n")
	b.WriteString("\t\t\tcontinue\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t\tlower := strings.ToLower(trimmed)\n")
	b.WriteString("\t\ts.counts[lower]++\n")
	b.WriteString("\t\tname := src.Name()\n")
	b.WriteString("\t\tstringer := src.Stringer()\n")
	b.WriteString("\t\trendered := stringer.String()\n")
	b.WriteString("\t\tjoined := fmt.Sprintf(\"%s::%s::%s\", name, rendered, lower)\n")
	b.WriteString("\t\tparts = append(parts, joined)\n")
	b.WriteString("\t}\n")
	b.WriteString("\tif len(parts) == 0 {\n")
	b.WriteString("\t\treturn \"\", errors.New(\"no items\")\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn strings.Join(parts, \",\"), nil\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// Register%d wires a handler key to a handler name and returns the prior\n", fileIdx)
	b.WriteString("// value if one existed. Adds method-declaration density to the file.\n")
	fmt.Fprintf(&b, "func (s *Service%d) Register%d(key, handler string) string {\n", fileIdx, fileIdx)
	b.WriteString("\tnormalized := strings.ToLower(strings.TrimSpace(key))\n")
	b.WriteString("\tprior := s.handlers[normalized]\n")
	b.WriteString("\ts.handlers[normalized] = strings.TrimSpace(handler)\n")
	b.WriteString("\treturn prior\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// Counts%d returns a copy of the per-key counts map.\n", fileIdx)
	fmt.Fprintf(&b, "func (s *Service%d) Counts%d() map[string]int {\n", fileIdx, fileIdx)
	b.WriteString("\tout := make(map[string]int, len(s.counts))\n")
	b.WriteString("\tfor k, v := range s.counts {\n")
	b.WriteString("\t\tout[k] = v\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn out\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// Name%d implements the Source interface; lets a Service satisfy Source.\n", fileIdx)
	fmt.Fprintf(&b, "func (s *Service%d) Name() string { return s.name }\n\n", fileIdx)

	fmt.Fprintf(&b, "// Stringer%d returns a fmt.Stringer view of this service.\n", fileIdx)
	fmt.Fprintf(&b, "func (s *Service%d) Stringer() fmt.Stringer { return stringerImpl(s.name) }\n\n", fileIdx)

	fmt.Fprintf(&b, "// Combine%d takes two same-package interfaces and merges their data through a\n", fileIdx)
	b.WriteString("// pipeline of standard library calls so the imported variable type index has\n")
	b.WriteString("// non-trivial scope-binding work to do per file.\n")
	fmt.Fprintf(&b, "func Combine%d(left, right Source%d) (Source%d, error) {\n", fileIdx, packageIdx, packageIdx)
	b.WriteString("\tif left == nil || right == nil {\n")
	b.WriteString("\t\treturn nil, errors.New(\"nil input\")\n")
	b.WriteString("\t}\n")
	b.WriteString("\tlname := left.Name()\n")
	b.WriteString("\trname := right.Name()\n")
	b.WriteString("\tljoined := strings.Join([]string{lname, rname}, \"+\")\n")
	b.WriteString("\tlower := strings.ToLower(strings.TrimSpace(ljoined))\n")
	fmt.Fprintf(&b, "\tservice := NewService%d(lower)\n", fileIdx)
	b.WriteString("\tservice.Register" + fmt.Sprintf("%d", fileIdx) + "(lname, rname)\n")
	b.WriteString("\treturn service, nil\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// Generic%d constrains its type parameter by Source%d so the generic\n", fileIdx, packageIdx)
	b.WriteString("// constraint prescan walks type_parameter_declaration with non-empty input.\n")
	fmt.Fprintf(&b, "func Generic%d[T Source%d](items []T, prefix string) []string {\n", fileIdx, packageIdx)
	b.WriteString("\tresult := make([]string, 0, len(items))\n")
	b.WriteString("\tfor _, item := range items {\n")
	b.WriteString("\t\tname := item.Name()\n")
	b.WriteString("\t\tstringer := item.Stringer()\n")
	b.WriteString("\t\trendered := stringer.String()\n")
	b.WriteString("\t\tjoined := strings.TrimSpace(prefix) + \"/\" + name + \"/\" + rendered\n")
	b.WriteString("\t\tresult = append(result, strings.ToLower(joined))\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn result\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// helper%d is a private helper that adds receiver-bound calls so the parent\n", fileIdx)
	b.WriteString("// lookup index has non-trivial work to do per file.\n")
	fmt.Fprintf(&b, "type helper%d struct{ value string }\n\n", fileIdx)
	fmt.Fprintf(&b, "func (h *helper%d) Render(prefix string) string {\n", fileIdx)
	b.WriteString("\tif h == nil {\n")
	b.WriteString("\t\treturn prefix\n")
	b.WriteString("\t}\n")
	b.WriteString("\ttrimmed := strings.TrimSpace(h.value)\n")
	b.WriteString("\tlower := strings.ToLower(trimmed)\n")
	b.WriteString("\treturn fmt.Sprintf(\"%s::%s\", prefix, lower)\n")
	b.WriteString("}\n")
	return b.String()
}
