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
	largeStateRegressionBytes = 2 << 20
	largeStateProofBytes      = 100 << 20
)

func TestParserStreamingPathDoesNotCallJSONUnmarshal(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	packageDir := filepath.Dir(currentFile)
	for _, fileName := range []string{
		"attributes.go",
		"json_token.go",
		"modules.go",
		"outputs.go",
		"parser.go",
		"providers.go",
		"resources.go",
		"snapshot_identity.go",
		"warnings.go",
	} {
		assertNoJSONUnmarshalCall(t, filepath.Join(packageDir, fileName))
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
	const maxPeakHeapGrowthBytes = 12 << 20
	if peakHeapGrowth > maxPeakHeapGrowthBytes {
		t.Fatalf("Parse() peak heap growth = %d bytes, want at most %d", peakHeapGrowth, maxPeakHeapGrowthBytes)
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
	const maxPeakHeapGrowthBytes = 96 << 20
	if peakHeapGrowth > maxPeakHeapGrowthBytes {
		t.Fatalf("Parse() peak heap growth = %d bytes, want at most %d", peakHeapGrowth, maxPeakHeapGrowthBytes)
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
	const maxPeakHeapGrowthBytes = 24 << 20
	if peakHeapGrowth > maxPeakHeapGrowthBytes {
		t.Fatalf("Parse() peak heap growth = %d bytes, want at most %d", peakHeapGrowth, maxPeakHeapGrowthBytes)
	}
}

func assertNoJSONUnmarshalCall(t *testing.T, path string) {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := goparser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("ParseFile(%q) error = %v, want nil", path, err)
	}
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
