package terraformstate_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestParseStream_PeakMemoryGate is the explicit hard gate for the
// streaming-parse memory guarantee. It fails CI if a refactor re-introduces
// full-payload buffering in terraformstate.ParseStream.
//
// The fixture is a 20k-resource synthetic state (~2 MB on disk). Peak heap
// growth during the stream must stay under maxStreamResourcePeakHeapGrowth
// (48 MB), which is comfortably above the working set ParseStream needs for
// its largest scalar plus the emitted fact batch but well below what a
// non-streaming parser would allocate.
//
// Related coverage:
//   - TestParseStreamLargeStateDoesNotRetainProviderBindingsOrWarnings in
//     parser_stream_memory_test.go asserts the same ceiling against a
//     richer fixture that exercises provider bindings and warnings.
//   - TestParseStreamLargeState100MiBStreamingProof in parser_memory_test.go
//     (env-gated by ESHU_TFSTATE_100MIB_PROOF=true) runs the assertion
//     against a 100 MB synthetic state for periodic large-scale validation.
//   - BenchmarkParseStream_LargeState in parser_bench_test.go reports
//     allocations and throughput for trend tracking.
//
// Do not call t.Parallel here. Parallel tests perturb runtime.MemStats and
// the measurement loses its meaning.
func TestParseStream_PeakMemoryGate(t *testing.T) {
	const resourceInstances = 20_000

	options := parseFixtureOptions(t)
	var emitted int64
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		result, err := terraformstate.ParseStream(
			context.Background(),
			largeResourceInstancesStateReader(resourceInstances),
			options,
			terraformstate.FactSinkFunc(func(context.Context, facts.Envelope) error {
				emitted++
				return nil
			}),
		)
		if err != nil {
			t.Fatalf("ParseStream() error = %v, want nil", err)
		}
		if got := result.ResourceFacts; got != resourceInstances {
			t.Fatalf("ParseStream() ResourceFacts = %d, want %d", got, resourceInstances)
		}
	})

	if peakHeapGrowth > maxStreamResourcePeakHeapGrowth {
		t.Fatalf("ParseStream() peak heap growth = %d bytes, want at most %d (maxStreamResourcePeakHeapGrowth); a regression has likely re-introduced full-payload buffering", peakHeapGrowth, maxStreamResourcePeakHeapGrowth)
	}
	if emitted == 0 {
		t.Fatalf("ParseStream() emitted 0 facts via FactSink, want at least 1")
	}
}
