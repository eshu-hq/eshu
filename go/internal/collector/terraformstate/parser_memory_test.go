package terraformstate_test

import (
	"context"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	largeStateRegressionBytes       = 32 << 20
	largeStateProofBytes            = 100 << 20
	maxIgnoredPayloadPeakHeapGrowth = 24 << 20
	maxStreamResourcePeakHeapGrowth = 48 << 20
	maxResourceParserPeakHeapGrowth = 96 << 20
)

func TestParserStreamingPathDoesNotCallJSONUnmarshal(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	packageDir := filepath.Dir(currentFile)
	for _, fileName := range []string{
		"anchors.go",
		"attributes.go",
		"json_token.go",
		"modules.go",
		"outputs.go",
		"parser.go",
		"providers.go",
		"resources.go",
		"snapshot_identity.go",
		"tags.go",
		"warnings.go",
	} {
		assertNoJSONUnmarshalCall(t, filepath.Join(packageDir, fileName))
	}
}

func TestParserStateDoesNotRetainDelayedFactFamilies(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	packageDir := filepath.Dir(currentFile)
	parsed := parseGoFile(t, filepath.Join(packageDir, "parser.go"))
	for _, fieldName := range []string{"warnings", "modules", "providerBindings"} {
		if stateParserHasField(parsed, fieldName) {
			t.Fatalf("stateParser retains delayed fact family field %q", fieldName)
		}
	}
}

func TestParserLargeStateStreamsIgnoredTopLevelPayload(t *testing.T) {
	options := parseFixtureOptions(t)
	var result terraformstate.ParseResult
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		var err error
		result, err = terraformstate.Parse(
			context.Background(),
			largeIgnoredPayloadStateReader(largeStateRegressionBytes),
			options,
		)
		if err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
	})

	requireFactKinds(t, result.Facts, facts.TerraformStateSnapshotFactKind)
	if got := len(result.Facts); got != 1 {
		t.Fatalf("Parse() emitted %d facts, want 1", got)
	}
	if peakHeapGrowth > maxIgnoredPayloadPeakHeapGrowth {
		t.Fatalf("Parse() peak heap growth = %d bytes, want at most %d", peakHeapGrowth, maxIgnoredPayloadPeakHeapGrowth)
	}
}

func TestParserLargeStateStreamsResourceInstances(t *testing.T) {
	const resourceInstances = 10_000

	options := parseFixtureOptions(t)
	var result terraformstate.ParseResult
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		var err error
		result, err = terraformstate.Parse(
			context.Background(),
			largeResourceInstancesStateReader(resourceInstances),
			options,
		)
		if err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
	})

	requireFactKinds(t, result.Facts, facts.TerraformStateSnapshotFactKind, facts.TerraformStateResourceFactKind)
	if got := result.ResourceFacts; got != resourceInstances {
		t.Fatalf("ResourceFacts = %d, want %d", got, resourceInstances)
	}
	if got, want := len(result.Facts), resourceInstances+1; got != want {
		t.Fatalf("Parse() emitted %d facts, want %d", got, want)
	}
	if peakHeapGrowth > maxResourceParserPeakHeapGrowth {
		t.Fatalf("Parse() peak heap growth = %d bytes, want at most %d", peakHeapGrowth, maxResourceParserPeakHeapGrowth)
	}
}

// TestParseStream_PeakMemoryGate is the CI hard gate for the streaming-parse
// memory guarantee in terraformstate.ParseStream. It asserts two invariants
// on a 20k-resource synthetic state:
//
//  1. peak heap growth stays under maxStreamResourcePeakHeapGrowth (48 MB);
//     a refactor that re-introduces full-payload buffering fails here.
//  2. resource facts are streamed through the FactSink rather than retained
//     in the parser; total fact count and per-kind count match the input.
//
// Companion coverage:
//   - TestParseStreamLargeStateDoesNotRetainProviderBindingsOrWarnings in
//     parser_stream_memory_test.go asserts the same ceiling against a
//     richer fixture exercising provider bindings and warnings.
//   - TestParseStreamLargeState100MiBStreamingProof (env-gated by
//     ESHU_TFSTATE_100MIB_PROOF=true) runs the assertion against a 100 MB
//     synthetic state for periodic large-scale validation.
//   - BenchmarkParseStream_LargeState in parser_bench_test.go reports
//     allocations and throughput for trend tracking.
//
// Do not call t.Parallel here. Parallel tests perturb runtime.MemStats and
// the measurement loses its meaning.
func TestParseStream_PeakMemoryGate(t *testing.T) {
	const resourceInstances = 20_000

	options := parseFixtureOptions(t)
	var count int
	var resourceFacts int64
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		result, err := terraformstate.ParseStream(
			context.Background(),
			largeResourceInstancesStateReader(resourceInstances),
			options,
			terraformstate.FactSinkFunc(func(_ context.Context, envelope facts.Envelope) error {
				count++
				if envelope.FactKind == facts.TerraformStateResourceFactKind {
					resourceFacts++
				}
				return nil
			}),
		)
		if err != nil {
			t.Fatalf("ParseStream() error = %v, want nil", err)
		}
		if got := result.ResourceFacts; got != resourceInstances {
			t.Fatalf("ResourceFacts = %d, want %d", got, resourceInstances)
		}
	})

	if got, want := count, resourceInstances+1; got != want {
		t.Fatalf("ParseStream() emitted %d facts, want %d", got, want)
	}
	if got := resourceFacts; got != resourceInstances {
		t.Fatalf("streamed resource facts = %d, want %d", got, resourceInstances)
	}
	if peakHeapGrowth > maxStreamResourcePeakHeapGrowth {
		t.Fatalf("ParseStream() peak heap growth = %d bytes, want at most %d (maxStreamResourcePeakHeapGrowth); a regression has likely re-introduced full-payload buffering", peakHeapGrowth, maxStreamResourcePeakHeapGrowth)
	}
}

// TestParseStream_PeakMemoryGate_CompositeCapture is the ADR-named acceptance
// gate for the streaming nested walker. It asserts the 48 MB
// maxStreamResourcePeakHeapGrowth ceiling still holds when every one of the
// 20k synthetic resource instances carries a populated
// server_side_encryption_configuration composite that the walker captures.
//
// A regression here means the walker has stopped being streaming — either by
// buffering the whole composite via decoder.Decode(&v any), or by retaining
// captured composites in parser state instead of streaming them to the
// FactSink. Pair with TestParseStream_PeakMemoryGate (no composites) and
// BenchmarkParseStreamComposite_LargeState to triangulate the regression.
//
// Do not call t.Parallel here. Parallel tests perturb runtime.MemStats and
// the measurement loses its meaning.
func TestParseStream_PeakMemoryGate_CompositeCapture(t *testing.T) {
	const resourceInstances = 20_000

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "server_side_encryption_configuration"},
		[2]string{"aws_s3_bucket", "rule"},
		[2]string{"aws_s3_bucket", "apply_server_side_encryption_by_default"},
		[2]string{"aws_s3_bucket", "sse_algorithm"},
	)
	var count int
	var resourceFacts int64
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		result, err := terraformstate.ParseStream(
			context.Background(),
			largeCompositeResourceInstancesStateReader(resourceInstances),
			options,
			terraformstate.FactSinkFunc(func(_ context.Context, envelope facts.Envelope) error {
				count++
				if envelope.FactKind == facts.TerraformStateResourceFactKind {
					resourceFacts++
				}
				return nil
			}),
		)
		if err != nil {
			t.Fatalf("ParseStream() error = %v, want nil", err)
		}
		if got := result.ResourceFacts; got != resourceInstances {
			t.Fatalf("ResourceFacts = %d, want %d", got, resourceInstances)
		}
	})

	if got, want := count, resourceInstances+1; got != want {
		t.Fatalf("ParseStream() emitted %d facts, want %d", got, want)
	}
	if got := resourceFacts; got != resourceInstances {
		t.Fatalf("streamed resource facts = %d, want %d", got, resourceInstances)
	}
	if peakHeapGrowth > maxStreamResourcePeakHeapGrowth {
		t.Fatalf("ParseStream() peak heap growth = %d bytes, want at most %d (maxStreamResourcePeakHeapGrowth); the composite-capture walker may have stopped streaming", peakHeapGrowth, maxStreamResourcePeakHeapGrowth)
	}
}

func TestParserLargeState100MiBStreamingProof(t *testing.T) {
	if os.Getenv("ESHU_TFSTATE_100MIB_PROOF") != "true" {
		t.Skip("set ESHU_TFSTATE_100MIB_PROOF=true to run the 100 MiB parser proof")
	}

	var result terraformstate.ParseResult
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		var err error
		result, err = terraformstate.Parse(
			context.Background(),
			largeIgnoredPayloadStateReader(largeStateProofBytes),
			parseFixtureOptions(t),
		)
		if err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
	})
	if got := len(result.Facts); got != 1 {
		t.Fatalf("Parse() emitted %d facts, want 1", got)
	}
	if peakHeapGrowth > maxIgnoredPayloadPeakHeapGrowth {
		t.Fatalf("Parse() peak heap growth = %d bytes, want at most %d", peakHeapGrowth, maxIgnoredPayloadPeakHeapGrowth)
	}
}

func TestParseStreamLargeState100MiBStreamingProof(t *testing.T) {
	if os.Getenv("ESHU_TFSTATE_100MIB_PROOF") != "true" {
		t.Skip("set ESHU_TFSTATE_100MIB_PROOF=true to run the 100 MiB parser proof")
	}

	var count int
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		_, err := terraformstate.ParseStream(
			context.Background(),
			largeIgnoredPayloadStateReader(largeStateProofBytes),
			parseFixtureOptions(t),
			terraformstate.FactSinkFunc(func(context.Context, facts.Envelope) error {
				count++
				return nil
			}),
		)
		if err != nil {
			t.Fatalf("ParseStream() error = %v, want nil", err)
		}
	})
	if got := count; got != 1 {
		t.Fatalf("ParseStream() emitted %d facts, want 1", got)
	}
	if peakHeapGrowth > maxIgnoredPayloadPeakHeapGrowth {
		t.Fatalf("ParseStream() peak heap growth = %d bytes, want at most %d", peakHeapGrowth, maxIgnoredPayloadPeakHeapGrowth)
	}
}

func assertNoJSONUnmarshalCall(t *testing.T, path string) {
	t.Helper()

	fileSet, parsed := parseGoFileWithSet(t, path)
	jsonAliases := map[string]bool{}
	for _, imported := range parsed.Imports {
		if strings.Trim(imported.Path.Value, `"`) != "encoding/json" {
			continue
		}
		alias := "json"
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		jsonAliases[alias] = true
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Unmarshal" {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if ok && jsonAliases[receiver.Name] {
			position := fileSet.Position(selector.Pos())
			t.Errorf("parser path calls encoding/json.Unmarshal at %s", position)
		}
		return true
	})
}

func parseGoFile(t *testing.T, path string) *ast.File {
	t.Helper()
	_, parsed := parseGoFileWithSet(t, path)
	return parsed
}

func parseGoFileWithSet(t *testing.T, path string) (*token.FileSet, *ast.File) {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := goparser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("ParseFile(%q) error = %v, want nil", path, err)
	}
	return fileSet, parsed
}

func stateParserHasField(parsed *ast.File, fieldName string) bool {
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "stateParser" {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				return false
			}
			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					if name.Name == fieldName {
						return true
					}
				}
			}
		}
	}
	return false
}

func measurePeakHeapGrowth(t *testing.T, run func()) uint64 {
	t.Helper()

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var maxHeap uint64
	atomic.StoreUint64(&maxHeap, before.HeapAlloc)
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				recordHeapSample(&maxHeap)
			}
		}
	}()
	run()
	recordHeapSample(&maxHeap)
	peak := atomic.LoadUint64(&maxHeap)
	if peak <= before.HeapAlloc {
		return 0
	}
	return peak - before.HeapAlloc
}

func recordHeapSample(maxHeap *uint64) {
	var sample runtime.MemStats
	runtime.ReadMemStats(&sample)
	for {
		current := atomic.LoadUint64(maxHeap)
		if sample.HeapAlloc <= current {
			return
		}
		if atomic.CompareAndSwapUint64(maxHeap, current, sample.HeapAlloc) {
			return
		}
	}
}

func largeIgnoredPayloadStateReader(payloadBytes int64) io.Reader {
	const prefix = `{"serial":17,"lineage":"lineage-123","checks":[`
	const suffix = `],"resources":[]}`
	return io.MultiReader(
		strings.NewReader(prefix),
		newRepeatedJSONArrayReader(`{"address":"module.api.aws_instance.web","status":"pass","details":{"messages":["ok","still-ok"],"nested":[{"severity":"low","count":3}]}}`, payloadBytes),
		strings.NewReader(suffix),
	)
}

func largeResourceInstancesStateReader(instanceCount int) io.Reader {
	const prefix = `{"serial":17,"lineage":"lineage-123","resources":[{"mode":"managed","type":"aws_instance","name":"web","instances":[`
	const suffix = `]}]}`
	return io.MultiReader(
		strings.NewReader(prefix),
		newRepeatedCountJSONArrayReader(`{"attributes":{"id":"i-1234567890","instance_type":"t3.micro","private_ip":"10.0.0.10"}}`, instanceCount),
		strings.NewReader(suffix),
	)
}

// largeCompositeResourceInstancesStateReader returns a synthetic state JSON
// where every one of instanceCount instances ships a populated
// server_side_encryption_configuration nested block. Used by the
// composite-capture memory gate to prove the streaming nested walker keeps
// peak heap growth under maxStreamResourcePeakHeapGrowth even when every
// instance triggers the walker.
func largeCompositeResourceInstancesStateReader(instanceCount int) io.Reader {
	const prefix = `{"serial":17,"lineage":"lineage-123","resources":[{"mode":"managed","type":"aws_s3_bucket","name":"logs","instances":[`
	const suffix = `]}]}`
	const instance = `{"attributes":{"bucket":"eshu-bucket","server_side_encryption_configuration":[{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"AES256"}]}]}]}}`
	return io.MultiReader(
		strings.NewReader(prefix),
		newRepeatedCountJSONArrayReader(instance, instanceCount),
		strings.NewReader(suffix),
	)
}

type repeatedJSONArrayReader struct {
	element   string
	target    int64
	written   int64
	needComma bool
	offset    int
	current   string
}

type repeatedCountJSONArrayReader struct {
	element   string
	count     int
	written   int
	needComma bool
	offset    int
	current   string
}

func newRepeatedCountJSONArrayReader(element string, count int) *repeatedCountJSONArrayReader {
	return &repeatedCountJSONArrayReader{element: element, count: count}
}

func (r *repeatedCountJSONArrayReader) Read(target []byte) (int, error) {
	if len(target) == 0 {
		return 0, nil
	}
	if r.current == "" {
		if r.written >= r.count {
			return 0, io.EOF
		}
		if r.needComma {
			r.current = "," + r.element
		} else {
			r.current = r.element
			r.needComma = true
		}
		r.offset = 0
		r.written++
	}
	n := copy(target, r.current[r.offset:])
	r.offset += n
	if r.offset == len(r.current) {
		r.current = ""
	}
	return n, nil
}

func newRepeatedJSONArrayReader(element string, targetBytes int64) *repeatedJSONArrayReader {
	return &repeatedJSONArrayReader{element: element, target: targetBytes}
}

func (r *repeatedJSONArrayReader) Read(target []byte) (int, error) {
	if len(target) == 0 {
		return 0, nil
	}
	if r.current == "" {
		if r.written >= r.target {
			return 0, io.EOF
		}
		if r.needComma {
			r.current = "," + r.element
		} else {
			r.current = r.element
			r.needComma = true
		}
		r.offset = 0
	}
	n := copy(target, r.current[r.offset:])
	r.offset += n
	r.written += int64(n)
	if r.offset == len(r.current) {
		r.current = ""
	}
	return n, nil
}
