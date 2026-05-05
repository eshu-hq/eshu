package main

import "testing"

func TestLocalContentSearchIndexesReadyRequiresExpectedProjectors(t *testing.T) {
	state := localContentSearchIndexDrainState{
		TotalWork:                9,
		OpenWork:                 0,
		CompletedProjectorWork:   1,
		OpenSharedProjectionWork: 0,
	}

	if localContentSearchIndexesReadyFromState(state, 2) {
		t.Fatal("ready = true with only one completed projector, want false")
	}

	state.CompletedProjectorWork = 2
	if !localContentSearchIndexesReadyFromState(state, 2) {
		t.Fatal("ready = false after expected projectors completed, want true")
	}
}

func TestLocalContentSearchIndexesReadyStillRequiresCleanDrain(t *testing.T) {
	state := localContentSearchIndexDrainState{
		TotalWork:                9,
		OpenWork:                 1,
		CompletedProjectorWork:   2,
		OpenSharedProjectionWork: 0,
	}
	if localContentSearchIndexesReadyFromState(state, 2) {
		t.Fatal("ready = true with open work, want false")
	}

	state.OpenWork = 0
	state.OpenSharedProjectionWork = 1
	if localContentSearchIndexesReadyFromState(state, 2) {
		t.Fatal("ready = true with open shared projection work, want false")
	}

	state.OpenSharedProjectionWork = 0
	state.TotalWork = 0
	if localContentSearchIndexesReadyFromState(state, 2) {
		t.Fatal("ready = true with no queue evidence, want false")
	}
}

func TestLocalContentSearchIndexExpectedProjectorsUsesFilesystemDiscovery(t *testing.T) {
	originalDiscover := localContentSearchDiscoverRepos
	t.Cleanup(func() {
		localContentSearchDiscoverRepos = originalDiscover
	})

	localContentSearchDiscoverRepos = func(root string) ([]string, error) {
		if root != "/workspace" {
			t.Fatalf("root = %q, want /workspace", root)
		}
		return []string{"api", "web"}, nil
	}

	got, err := localContentSearchIndexExpectedProjectors("/workspace")
	if err != nil {
		t.Fatalf("localContentSearchIndexExpectedProjectors() error = %v, want nil", err)
	}
	if got != 2 {
		t.Fatalf("localContentSearchIndexExpectedProjectors() = %d, want 2", got)
	}
}
