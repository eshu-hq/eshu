// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestNewValueFlowFixpointProjectorWiresCloudSinkGraphLoader(t *testing.T) {
	t.Parallel()

	componentStore := stubValueFlowFixpointComponentStore{}
	projector := newValueFlowFixpointProjector(nil, nil, nil, componentStore, stubCypherReader{}, nil, nil)
	loader, ok := projector.Loader.(reducer.ValueFlowFixpointEvidenceLoader)
	if !ok {
		t.Fatalf("projector loader = %T, want reducer.ValueFlowFixpointEvidenceLoader", projector.Loader)
	}
	if _, ok := loader.FixpointComponentStore.(stubValueFlowFixpointComponentStore); !ok {
		t.Fatalf("component store = %T, want stubValueFlowFixpointComponentStore", loader.FixpointComponentStore)
	}
	cloudLoader, ok := loader.CloudSinkSnapshotLoader.(reducer.GraphValueFlowCloudSinkTargetLoader)
	if !ok {
		t.Fatalf("cloud sink loader = %T, want reducer.GraphValueFlowCloudSinkTargetLoader", loader.CloudSinkSnapshotLoader)
	}
	if _, ok := cloudLoader.Graph.(stubCypherReader); !ok {
		t.Fatalf("cloud sink graph reader = %T, want stubCypherReader", cloudLoader.Graph)
	}
}

type stubValueFlowFixpointComponentStore struct{}

func (stubValueFlowFixpointComponentStore) LoadValueFlowFixpointComponents(
	context.Context,
	[]string,
) (map[string]interproc.Result, error) {
	return nil, nil
}

func (stubValueFlowFixpointComponentStore) StoreValueFlowFixpointComponents(
	context.Context,
	map[string]interproc.Result,
) error {
	return nil
}
