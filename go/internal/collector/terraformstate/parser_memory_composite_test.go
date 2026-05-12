package terraformstate_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

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

// TestParserCapturesLargeScalarLeafUnderBoundedMemory is the Q3 evidence the
// composite-capture ADR
// (docs/docs/adrs/2026-05-12-tfstate-parser-composite-capture-for-schema-known-paths.md
// §Edge Cases, row "A SchemaKnown composite whose value is genuinely large")
// commits to. It proves the streaming walker (a) captures a 10k-character
// scalar leaf intact, no truncation and no drop, and (b) keeps the parse's
// total heap growth under the same 48 MB ceiling the streaming invariant
// uses. A regression here means either the walker started buffering whole
// composites or someone introduced a silent size cap on captured scalars.
//
// Do not call t.Parallel. Parallel tests perturb runtime.MemStats and the
// heap-growth measurement loses its meaning.
func TestParserCapturesLargeScalarLeafUnderBoundedMemory(t *testing.T) {
	const leafSize = 10_000

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "server_side_encryption_configuration"},
		[2]string{"aws_s3_bucket", "rule"},
		[2]string{"aws_s3_bucket", "apply_server_side_encryption_by_default"},
		[2]string{"aws_s3_bucket", "sse_algorithm"},
	)

	largeLeaf := strings.Repeat("x", leafSize)
	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_s3_bucket",
			"name":"logs",
			"instances":[{
				"attributes":{
					"server_side_encryption_configuration":[
						{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"` + largeLeaf + `"}]}]}
					]
				}
			}]
		}]
	}`

	var result terraformstate.ParseResult
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		var err error
		result, err = terraformstate.Parse(context.Background(), strings.NewReader(state), options)
		if err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
	})

	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	composite, ok := attributes["server_side_encryption_configuration"].([]any)
	if !ok || len(composite) != 1 {
		t.Fatalf("attributes[server_side_encryption_configuration] = %#v, want []any of length 1", attributes["server_side_encryption_configuration"])
	}
	rule, ok := composite[0].(map[string]any)
	if !ok {
		t.Fatalf("composite[0] = %#v, want map[string]any", composite[0])
	}
	ruleList, ok := rule["rule"].([]any)
	if !ok || len(ruleList) != 1 {
		t.Fatalf("rule[rule] = %#v, want []any of length 1", rule["rule"])
	}
	ruleMap, ok := ruleList[0].(map[string]any)
	if !ok {
		t.Fatalf("ruleList[0] = %#v, want map[string]any", ruleList[0])
	}
	applyList, ok := ruleMap["apply_server_side_encryption_by_default"].([]any)
	if !ok || len(applyList) != 1 {
		t.Fatalf("ruleMap[apply_server_side_encryption_by_default] = %#v, want []any of length 1", ruleMap["apply_server_side_encryption_by_default"])
	}
	applyMap, ok := applyList[0].(map[string]any)
	if !ok {
		t.Fatalf("applyList[0] = %#v, want map[string]any", applyList[0])
	}
	got, ok := applyMap["sse_algorithm"].(string)
	if !ok {
		t.Fatalf("applyMap[sse_algorithm] = %#v, want string", applyMap["sse_algorithm"])
	}
	if len(got) != leafSize {
		t.Fatalf("len(applyMap[sse_algorithm]) = %d, want %d (walker dropped or truncated a SchemaKnown scalar leaf)", len(got), leafSize)
	}
	if got != largeLeaf {
		t.Fatalf("applyMap[sse_algorithm] differs from input (walker mutated the scalar leaf)")
	}
	if peakHeapGrowth > maxStreamResourcePeakHeapGrowth {
		t.Fatalf("Parse() peak heap growth = %d bytes, want at most %d (maxStreamResourcePeakHeapGrowth); a 10k-character SchemaKnown scalar leaf must stay under the streaming ceiling", peakHeapGrowth, maxStreamResourcePeakHeapGrowth)
	}
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
