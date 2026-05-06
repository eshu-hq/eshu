package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLocalIaCReachabilityFinalizerReadyRequiresFullDrain(t *testing.T) {
	state := localContentSearchIndexDrainState{
		TotalWork:                12,
		OpenWork:                 0,
		CompletedProjectorWork:   1,
		OpenSharedProjectionWork: 0,
	}
	if localIaCReachabilityFinalizerReadyFromState(state, 2) {
		t.Fatal("ready = true before every expected source-local projector completed")
	}

	state.CompletedProjectorWork = 2
	state.OpenSharedProjectionWork = 1
	if localIaCReachabilityFinalizerReadyFromState(state, 2) {
		t.Fatal("ready = true with open shared projection work")
	}

	state.OpenSharedProjectionWork = 0
	if !localIaCReachabilityFinalizerReadyFromState(state, 2) {
		t.Fatal("ready = false after fact queue and shared projection drained")
	}
}

func TestRunLocalIaCReachabilityFinalizerMaterializesOnceWhenReady(t *testing.T) {
	originalQuery := queryLocalIaCReachabilityDrainState
	t.Cleanup(func() {
		queryLocalIaCReachabilityDrainState = originalQuery
	})
	stateDB := &localIaCFinalizerTestDB{
		states: []localContentSearchIndexDrainState{
			{
				TotalWork:                3,
				OpenWork:                 1,
				CompletedProjectorWork:   1,
				OpenSharedProjectionWork: 0,
			},
			{
				TotalWork:                3,
				OpenWork:                 0,
				CompletedProjectorWork:   2,
				OpenSharedProjectionWork: 0,
			},
		},
	}
	queryLocalIaCReachabilityDrainState = func(context.Context, localContentSearchIndexDB) (localContentSearchIndexDrainState, error) {
		return stateDB.nextState(), nil
	}

	var materialized int
	err := runLocalIaCReachabilityFinalizer(
		context.Background(),
		nil,
		2,
		time.Nanosecond,
		func(context.Context) error {
			materialized++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("runLocalIaCReachabilityFinalizer() error = %v, want nil", err)
	}
	if materialized != 1 {
		t.Fatalf("materialized = %d, want 1", materialized)
	}
}

func TestRunLocalIaCReachabilityFinalizerReturnsMaterializeError(t *testing.T) {
	originalQuery := queryLocalIaCReachabilityDrainState
	t.Cleanup(func() {
		queryLocalIaCReachabilityDrainState = originalQuery
	})
	queryLocalIaCReachabilityDrainState = func(context.Context, localContentSearchIndexDB) (localContentSearchIndexDrainState, error) {
		return localContentSearchIndexDrainState{
			TotalWork:                1,
			OpenWork:                 0,
			CompletedProjectorWork:   1,
			OpenSharedProjectionWork: 0,
		}, nil
	}

	materializeErr := errors.New("boom")
	err := runLocalIaCReachabilityFinalizer(
		context.Background(),
		nil,
		1,
		time.Nanosecond,
		func(context.Context) error {
			return materializeErr
		},
	)
	if !errors.Is(err, materializeErr) {
		t.Fatalf("error = %v, want %v", err, materializeErr)
	}
}

type localIaCFinalizerTestDB struct {
	localContentSearchIndexDB
	states []localContentSearchIndexDrainState
	reads  int
}

func (db *localIaCFinalizerTestDB) nextState() localContentSearchIndexDrainState {
	if db.reads >= len(db.states) {
		return db.states[len(db.states)-1]
	}
	state := db.states[db.reads]
	db.reads++
	return state
}
