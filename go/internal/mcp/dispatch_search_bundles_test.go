package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// searchBundlesGraphRecorder records whether the bundle handler reached the
// graph. An unscoped request must be rejected before any Run/RunSingle call.
type searchBundlesGraphRecorder struct {
	queried *bool
}

func (r searchBundlesGraphRecorder) Run(
	_ context.Context,
	_ string,
	_ map[string]any,
) ([]map[string]any, error) {
	*r.queried = true
	return nil, nil
}

func (r searchBundlesGraphRecorder) RunSingle(
	_ context.Context,
	_ string,
	_ map[string]any,
) (map[string]any, error) {
	*r.queried = true
	return nil, nil
}

// TestDispatchToolSearchRegistryBundlesUnscopedReturnsEnvelopeIsError proves the
// MCP dispatch path turns an unscoped search_registry_bundles call into a
// structured canonical-envelope IsError result, not a transport error. #3520
// follow-up: dispatch sets Accept: application/eshu.envelope+json and only
// recognizes canonical envelopes (data/truth/error); a non-envelope 400 would
// degrade to `fmt.Errorf("HTTP 400 ...")` and surface as a tool transport
// failure instead of a structured tool error. The graph must never be touched.
func TestDispatchToolSearchRegistryBundlesUnscopedReturnsEnvelopeIsError(t *testing.T) {
	t.Parallel()

	queried := false
	handler := &query.CodeHandler{
		Neo4j:   searchBundlesGraphRecorder{queried: &queried},
		Profile: query.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// resolveRoute forwards empty query/ecosystem strings on the MCP path, so
	// this models the exported-schema "no scope supplied" call shape.
	result, err := dispatchTool(
		context.Background(),
		mux,
		"search_registry_bundles",
		map[string]any{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool error = %v, want nil (structured envelope, not transport error)", err)
	}
	if result == nil {
		t.Fatal("dispatchTool result = nil, want structured result")
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for unscoped bundle request")
	}
	if result.Envelope == nil {
		t.Fatalf("result.Envelope = nil, want canonical envelope; value=%#v", result.Value)
	}
	if result.Envelope.Error == nil {
		t.Fatalf("result.Envelope.Error = nil, want populated error envelope")
	}
	if got, want := result.Envelope.Error.Code, query.ErrorCodeInvalidArgument; got != want {
		t.Fatalf("envelope error code = %q, want %q", got, want)
	}
	if queried {
		t.Fatalf("graph was queried for an unscoped bundle request; want bounded reject before scan")
	}
}
