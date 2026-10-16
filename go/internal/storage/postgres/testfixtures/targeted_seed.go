// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testfixtures

import (
	"encoding/json"
	"time"
)

// TargetedGCPScope is the cloud scope of the GCP fixtures. It carries
// gcp_cloud_relationship facts and no repository fact.
const TargetedGCPScope = "gcp:project:demo:relationship:global"

// TargetedArgoAppSet is an ApplicationSet whose git generator reads the
// external platform-config repository and whose template deploys the service
// each config file names (the shape of
// TestDiscoverArgoCDApplicationSetDeploySourceFromConfigServiceIdentity).
const TargetedArgoAppSet = `apiVersion: argoproj.io/v1alpha1
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

// SeedTargetedCorpus seeds the two repositories every fixture shares: the
// owed target payments-service and the dependency orders-api it references.
func SeedTargetedCorpus(p *TargetedDiffPair) {
	p.T.Helper()
	p.GitRepo("git:tgt", "tgt-1", "repo-tgt", "payments-service")
	p.WorkItems("git:tgt", "tgt-1")
	p.GitRepo("git:dep", "dep-1", "repo-dep", "orders-api")
	p.WorkItems("git:dep", "dep-1")
}

// QuietGeneration activates a new generation of a one-repository git scope
// without any maintenance pass: the quiet Ack an obligation records. Each
// alias in refs becomes one Terraform reference in the new generation.
func (p *TargetedDiffPair) QuietGeneration(scopeID, generationID, repoID, name string, refs ...string) {
	p.T.Helper()
	p.Generation(scopeID, generationID, 90*time.Minute, true)
	p.Repo(scopeID, generationID, repoID, name)
	for _, alias := range refs {
		p.TerraformRef(generationID+"-ref-"+alias, scopeID, generationID, repoID, "main.tf", alias)
	}
	p.WorkItems(scopeID, generationID)
}

// GCPRelation seeds a Cloud Run service -> Secret relation in the GCP scope
// whose names resolve to the source and target repositories.
func (p *TargetedDiffPair) GCPRelation(factID, generationID, sourceName, targetName string) {
	p.T.Helper()
	payload, err := json.Marshal(map[string]string{
		"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/" + sourceName,
		"source_asset_type":         "run.googleapis.com/Service",
		"relationship_type":         "run_service_uses_secret",
		"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/" + targetName,
		"target_asset_type":         "secretmanager.googleapis.com/Secret",
		"support_state":             "supported",
	})
	if err != nil {
		p.T.Fatalf("marshal gcp relation: %v", err)
	}
	p.Fact(factID, TargetedGCPScope, generationID, "gcp_cloud_relationship", string(payload))
}

// SeedTargetedArgoCD seeds the control repository holding the ApplicationSet
// and the external config repository whose config file names service.
func SeedTargetedArgoCD(p *TargetedDiffPair, service string) {
	p.T.Helper()
	p.GitRepo("git:gitops", "gitops-1", "repo-gitops", "platform-gitops")
	payload, err := json.Marshal(map[string]string{
		"repo_id":       "repo-gitops",
		"artifact_type": "argocd",
		"relative_path": "applicationsets/services.yaml",
		"content":       TargetedArgoAppSet,
	})
	if err != nil {
		p.T.Fatalf("marshal appset: %v", err)
	}
	p.Fact("gitops-1-appset", "git:gitops", "gitops-1", "content", string(payload))
	p.WorkItems("git:gitops", "gitops-1")
	p.GitRepo("git:config", "config-1", "repo-config", "platform-config")
	p.ArgoConfig("config-1", service)
	p.WorkItems("git:config", "config-1")
}

// ArgoConfig seeds the generator config file of the config repository.
func (p *TargetedDiffPair) ArgoConfig(generationID, service string) {
	p.T.Helper()
	payload, err := json.Marshal(map[string]string{
		"repo_id":       "repo-config",
		"artifact_type": "yaml",
		"relative_path": "services/prod/config.yaml",
		"content": "addon: " + service + "\nenvironment: prod\ngit:\n" +
			"  repoURL: https://github.com/myorg/platform-config.git\n  overlayPath: services/prod\n" +
			"helm:\n  repoURL: https://charts.example.invalid\n  chart: shared-service\n  releaseName: " + service + "\n",
	})
	if err != nil {
		p.T.Fatalf("marshal argo config: %v", err)
	}
	p.Fact(generationID+"-config", "git:config", generationID, "content", string(payload))
}

// SeedTargetedMonoGeneration activates mono-2: one scope and generation
// holding two repositories that each reference orders-api.
func SeedTargetedMonoGeneration(p *TargetedDiffPair) {
	p.T.Helper()
	p.Generation("git:mono", "mono-2", 90*time.Minute, true)
	p.Repo("git:mono", "mono-2", "repo-m1", "mono-alpha")
	p.Repo("git:mono", "mono-2", "repo-m2", "mono-beta")
	p.TerraformRef("mono-2-m1-ref", "git:mono", "mono-2", "repo-m1", "m1.tf", "orders-api")
	p.TerraformRef("mono-2-m2-ref", "git:mono", "mono-2", "repo-m2", "m2.tf", "orders-api")
	p.WorkItems("git:mono", "mono-2")
}
