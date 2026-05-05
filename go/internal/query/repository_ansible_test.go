package query

import "testing"

func TestBuildRepositoryInfrastructureOverviewCountsAnsibleArtifacts(t *testing.T) {
	t.Parallel()

	overview := buildRepositoryInfrastructureOverview(nil, []FileContent{
		{ArtifactType: "ansible_playbook"},
		{ArtifactType: "ansible_inventory"},
		{ArtifactType: "ansible_vars"},
		{ArtifactType: "ansible_role"},
		{ArtifactType: "ansible_task_entrypoint"},
	})
	if overview == nil {
		t.Fatal("buildRepositoryInfrastructureOverview() = nil, want artifact counts")
	}

	artifactCounts, ok := overview["artifact_family_counts"].(map[string]int)
	if !ok {
		t.Fatalf("artifact_family_counts type = %T, want map[string]int", overview["artifact_family_counts"])
	}
	if got, want := artifactCounts["ansible"], 5; got != want {
		t.Fatalf("artifact_family_counts[ansible] = %d, want %d", got, want)
	}
}
