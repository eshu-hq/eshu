package terraformstate_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkParseStream_LargeState measures throughput and allocations for the
// streaming Terraform-state parser. It exists for trend tracking: a refactor
// that re-introduces full-payload buffering will surface here as a sharp jump
// in B/op or allocs/op even if peak heap stays below the hard-gate ceiling
// in TestParseStream_PeakMemoryGate.
//
// Run with:
//
//	cd go && go test -bench=BenchmarkParseStream_LargeState -benchmem \
//	    -run=^$ ./internal/collector/terraformstate
//
// The synthetic state is regenerated on every iteration because
// largeResourceInstancesStateReader returns a one-shot io.Reader.
func BenchmarkParseStream_LargeState(b *testing.B) {
	for _, instanceCount := range []int{1_000, 10_000, 20_000} {
		b.Run(fmt.Sprintf("%d_instances", instanceCount), func(b *testing.B) {
			options := parseFixtureOptions(b)
			byteSize := measureFixtureBytes(b, instanceCount)
			b.SetBytes(byteSize)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := terraformstate.ParseStream(
					context.Background(),
					largeResourceInstancesStateReader(instanceCount),
					options,
					terraformstate.FactSinkFunc(func(context.Context, facts.Envelope) error {
						return nil
					}),
				)
				if err != nil {
					b.Fatalf("ParseStream() error = %v, want nil", err)
				}
			}
		})
	}
}

// measureFixtureBytes drains the fixture once to learn its exact byte size so
// that b.SetBytes reports honest throughput in MB/s. The reader is consumed
// here and the bench loop builds a fresh one per iteration.
func measureFixtureBytes(b *testing.B, instanceCount int) int64 {
	b.Helper()
	n, err := io.Copy(io.Discard, largeResourceInstancesStateReader(instanceCount))
	if err != nil {
		b.Fatalf("measure fixture bytes: %v", err)
	}
	return n
}
