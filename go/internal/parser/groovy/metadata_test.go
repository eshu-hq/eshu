package groovy

import "testing"

func TestPipelineMetadataExtractsJenkinsSignals(t *testing.T) {
	t.Parallel()

	source := `@Library('pipelines@v2') _
library identifier: 'shared-controllers@main'
pipelineDeploy(
  entry_point: 'dist/api.js',
  use_configd: true,
  pre_deploy: { sh 'ansible-playbook deploy.yml -i inventory/hosts --limit prod' }
)
`

	got := PipelineMetadata(source)
	assertStringSliceContains(t, got.SharedLibraries, "pipelines")
	assertStringSliceContains(t, got.SharedLibraries, "shared-controllers")
	assertStringSliceContains(t, got.PipelineCalls, "pipelineDeploy")
	assertStringSliceContains(t, got.EntryPoints, "dist/api.js")
	assertStringSliceContains(t, got.ShellCommands, "ansible-playbook deploy.yml -i inventory/hosts --limit prod")
	if got.UseConfigd == nil || *got.UseConfigd != true {
		t.Fatalf("UseConfigd = %#v, want true pointer", got.UseConfigd)
	}
	if !got.HasPreDeploy {
		t.Fatalf("HasPreDeploy = false, want true")
	}
	if len(got.AnsiblePlaybookHints) != 1 {
		t.Fatalf("AnsiblePlaybookHints len = %d, want 1: %#v", len(got.AnsiblePlaybookHints), got.AnsiblePlaybookHints)
	}
	if got.AnsiblePlaybookHints[0].Playbook != "deploy.yml" {
		t.Fatalf("Playbook = %q, want deploy.yml", got.AnsiblePlaybookHints[0].Playbook)
	}
	if got.AnsiblePlaybookHints[0].Inventory != "inventory/hosts" {
		t.Fatalf("Inventory = %#v, want inventory/hosts", got.AnsiblePlaybookHints[0].Inventory)
	}
}

func TestPipelineMetadataMapPreservesExistingPayloadShape(t *testing.T) {
	t.Parallel()

	got := PipelineMetadata(`pipelineDeploy(entry_point: 'deploy.sh')`).Map()
	if _, ok := got["use_configd"]; !ok {
		t.Fatalf("Map() missing use_configd key")
	}
	if _, ok := got["ansible_playbook_hints"].([]map[string]any); !ok {
		t.Fatalf("ansible_playbook_hints = %T, want []map[string]any", got["ansible_playbook_hints"])
	}
}

func assertStringSliceContains(t *testing.T, values []string, want string) {
	t.Helper()

	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%#v does not contain %q", values, want)
}
