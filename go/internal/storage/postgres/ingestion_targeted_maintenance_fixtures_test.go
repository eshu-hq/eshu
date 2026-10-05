// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"time"
)

// targetedGCPScope is the cloud scope of the GCP fixtures. It carries
// gcp_cloud_relationship facts and no repository fact.
const targetedGCPScope = "gcp:project:demo:relationship:global"

// targetedArgoAppSet is an ApplicationSet whose git generator reads the
// external platform-config repository and whose template deploys the service
// each config file names (the shape of
// TestDiscoverArgoCDApplicationSetDeploySourceFromConfigServiceIdentity).
const targetedArgoAppSet = `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/platform-config.git
        files:
          - path: services/*/config.yaml
  template:
    spec:
      sources:
        - repoURL: "{{ .helm.repoURL }}"
          chart: "{{ .helm.chart }}"
        - repoURL: "{{ .git.repoURL }}"
          path: "{{ .git.overlayPath }}"
`

// seedTargetedCorpus seeds the two repositories every fixture shares: the
// owed target payments-service and the dependency orders-api it references.
func seedTargetedCorpus(p *targetedDiffPair) {
	p.t.Helper()
	p.gitRepo("git:tgt", "tgt-1", "repo-tgt", "payments-service")
	p.workItems("git:tgt", "tgt-1")
	p.gitRepo("git:dep", "dep-1", "repo-dep", "orders-api")
	p.workItems("git:dep", "dep-1")
}

// quietGeneration activates a new generation of a one-repository git scope
// without any maintenance pass: the quiet Ack an obligation records. Each
// alias in refs becomes one Terraform reference in the new generation.
func (p *targetedDiffPair) quietGeneration(scopeID, generationID, repoID, name string, refs ...string) {
	p.t.Helper()
	p.generation(scopeID, generationID, 90*time.Minute, true)
	p.repo(scopeID, generationID, repoID, name)
	for _, alias := range refs {
		p.terraformRef(generationID+"-ref-"+alias, scopeID, generationID, repoID, "main.tf", alias)
	}
	p.workItems(scopeID, generationID)
}

// owedPartitions builds an owed partition list from scope/generation pairs.
func owedPartitions(pairs ...string) []scopeGenerationPartition {
	return sortedPartitions(partitionSet(pairs...))
}

// gcpRelation seeds a Cloud Run service -> Secret relation in the GCP scope
// whose names resolve to the source and target repositories.
func (p *targetedDiffPair) gcpRelation(factID, generationID, sourceName, targetName string) {
	p.t.Helper()
	payload, err := json.Marshal(map[string]string{
		"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/" + sourceName,
		"source_asset_type":         "run.googleapis.com/Service",
		"relationship_type":         "run_service_uses_secret",
		"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/" + targetName,
		"target_asset_type":         "secretmanager.googleapis.com/Secret",
		"support_state":             "supported",
	})
	if err != nil {
		p.t.Fatalf("marshal gcp relation: %v", err)
	}
	p.fact(factID, targetedGCPScope, generationID, "gcp_cloud_relationship", string(payload))
}

// seedTargetedArgoCD seeds the control repository holding the ApplicationSet
// and the external config repository whose config file names service.
func seedTargetedArgoCD(p *targetedDiffPair, service string) {
	p.t.Helper()
	p.gitRepo("git:gitops", "gitops-1", "repo-gitops", "platform-gitops")
	payload, err := json.Marshal(map[string]string{
		"repo_id":       "repo-gitops",
		"artifact_type": "argocd",
		"relative_path": "applicationsets/services.yaml",
		"content":       targetedArgoAppSet,
	})
	if err != nil {
		p.t.Fatalf("marshal appset: %v", err)
	}
	p.fact("gitops-1-appset", "git:gitops", "gitops-1", "content", string(payload))
	p.workItems("git:gitops", "gitops-1")
	p.gitRepo("git:config", "config-1", "repo-config", "platform-config")
	p.argoConfig("config-1", service)
	p.workItems("git:config", "config-1")
}

// argoConfig seeds the generator config file of the config repository.
func (p *targetedDiffPair) argoConfig(generationID, service string) {
	p.t.Helper()
	payload, err := json.Marshal(map[string]string{
		"repo_id":       "repo-config",
		"artifact_type": "yaml",
		"relative_path": "services/prod/config.yaml",
		"content": "addon: " + service + "\nenvironment: prod\ngit:\n" +
			"  repoURL: https://github.com/myorg/platform-config.git\n  overlayPath: services/prod\n" +
			"helm:\n  repoURL: https://charts.example.invalid\n  chart: shared-service\n  releaseName: " + service + "\n",
	})
	if err != nil {
		p.t.Fatalf("marshal argo config: %v", err)
	}
	p.fact(generationID+"-config", "git:config", generationID, "content", string(payload))
}

// seedTargetedMonoGeneration activates mono-2: one scope and generation
// holding two repositories that each reference orders-api.
func seedTargetedMonoGeneration(p *targetedDiffPair) {
	p.t.Helper()
	p.generation("git:mono", "mono-2", 90*time.Minute, true)
	p.repo("git:mono", "mono-2", "repo-m1", "mono-alpha")
	p.repo("git:mono", "mono-2", "repo-m2", "mono-beta")
	p.terraformRef("mono-2-m1-ref", "git:mono", "mono-2", "repo-m1", "m1.tf", "orders-api")
	p.terraformRef("mono-2-m2-ref", "git:mono", "mono-2", "repo-m2", "m2.tf", "orders-api")
	p.workItems("git:mono", "mono-2")
}
