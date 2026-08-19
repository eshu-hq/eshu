package backendconformance

import "testing"

func TestValueFlowPairIsWiredIntoDefaults(t *testing.T) {
	var readFound bool
	for _, c := range DefaultReadCorpus() {
		if c.Name == "value-flow cloud sink aggregation and subscript projection" {
			readFound = true
			if c.MinRows < 1 {
				t.Fatalf("value-flow read case MinRows = %d, want >= 1 so an empty result fails", c.MinRows)
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
