package backendconformance

import (
	"strings"
	"testing"
)

func TestValueFlowPairIsWiredIntoDefaults(t *testing.T) {
	var readFound bool
	for _, c := range DefaultReadCorpus() {
		if c.Name == "value-flow cloud sink aggregation and subscript projection" {
			readFound = true
			if c.MinRows < 1 {
				t.Fatalf("value-flow read case MinRows = %d, want >= 1 so an empty result fails", c.MinRows)
			}
			// Pin the three shapes this case exists to exercise. Without this
			// the query could be reduced to something trivial that keeps the
			// name and MinRows, and both this guard and every default CI run
			// would stay green while detecting nothing.
			for _, fragment := range []string{
				"collect(DISTINCT workload)",
				"workloads[0] AS workload",
				"IN sinkRel.actions",
			} {
				if !strings.Contains(c.Cypher, fragment) {
					t.Errorf("value-flow read case no longer contains %q; it must keep exercising all three divergent shapes", fragment)
				}
			}
		}
	}
	if !readFound {
		t.Fatal("value-flow read case is not in DefaultReadCorpus")
	}
	var writeFound bool
	for _, c := range DefaultWriteCorpus() {
		if c.Name == "value-flow cloud sink seed" {
			writeFound = true
			if !c.RequireAtomicGroup {
				t.Fatal("value-flow seed must commit atomically")
			}
		}
	}
	if !writeFound {
		t.Fatal("value-flow seed case is not in DefaultWriteCorpus")
	}
}
