package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestNewValueFlowFixpointProjectorWiresCloudSinkGraphLoader(t *testing.T) {
	t.Parallel()

	projector := newValueFlowFixpointProjector(nil, nil, nil, stubCypherReader{}, nil, nil)
	loader, ok := projector.Loader.(reducer.ValueFlowFixpointEvidenceLoader)
	if !ok {
		t.Fatalf("projector loader = %T, want reducer.ValueFlowFixpointEvidenceLoader", projector.Loader)
	}
	cloudLoader, ok := loader.CloudSinkSnapshotLoader.(reducer.GraphValueFlowCloudSinkTargetLoader)
	if !ok {
		t.Fatalf("cloud sink loader = %T, want reducer.GraphValueFlowCloudSinkTargetLoader", loader.CloudSinkSnapshotLoader)
	}
	if _, ok := cloudLoader.Graph.(stubCypherReader); !ok {
		t.Fatalf("cloud sink graph reader = %T, want stubCypherReader", cloudLoader.Graph)
	}
}
