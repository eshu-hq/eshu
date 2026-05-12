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
func BenchmarkPreScanGoPackageSemanticRoots(b *testing.B) {
	repoRoot := b.TempDir()
	paths := writePrescanBenchCorpus(b, repoRoot, 4, 50)

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
}

// writePrescanBenchCorpus materializes a synthetic Go module with packageCount
// directories of filesPerPackage Go files each, plus a go.mod at the root.
// Each file is shaped to engage every per-file extractor the parent prescan
// invokes; package-level interface declarations and method declarations sit
// alongside callers so chained-call and generic-constraint passes have
// non-trivial work to do.
func writePrescanBenchCorpus(b *testing.B, repoRoot string, packageCount, filesPerPackage int) []string {
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
			writeBenchFile(b, filePath, generatePrescanCallerFile(packageName, p, f, modulePath))
			paths = append(paths, filePath)
		}
	}
	return paths
}

// generatePrescanInterfaceFile produces an interface-bearing Go file shaped to
// engage LocalInterfaceMethods, LocalInterfaceImportedMethodReturns, and the
// MethodDeclarationKeys helpers. The local interface returns a fmt.Stringer so
// downstream ImportedDirectMethodCallRootsWithInterfaceReturns has work.
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
	b.WriteString("type stringerImpl string\n\n")
	b.WriteString("func (s stringerImpl) String() string { return string(s) }\n")
	return b.String()
}

// generatePrescanCallerFile produces a Go file shaped to engage the consumer
// and exported interface extractors. The file imports fmt, declares an
// exported function that takes a same-package interface parameter (exercising
// ExportedInterfaceParamMethods), calls a fmt.Stringer method on a returned
// value (exercising the chained imported direct method call roots), and
// declares a generic helper whose type parameter is constrained by the
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
	fmt.Fprintf(&b, "// Consume%d takes a same-package interface and exercises the exported\n", fileIdx)
	b.WriteString("// interface parameter prescan plus imported direct method calls.\n")
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
