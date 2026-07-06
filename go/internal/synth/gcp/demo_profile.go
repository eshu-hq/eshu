// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DemoOrgSeed is the stable public seed for the demo/conformance corpus
	// profile owned by issue #4747.
	DemoOrgSeed uint64 = 4592
	// DemoGitHubOrg is the fictional GitHub organization shared with the
	// golden-corpus gate's ESHU_GITHUB_ORG setting.
	DemoGitHubOrg = "acme"
	// DemoIdentityScheme documents the reserved cross-family repository remote
	// shape. Concrete remotes replace <repo> with a fixture repository name.
	DemoIdentityScheme = "github.com/acme/<repo>"
)

const (
	demoGCPProjectID      = "acme-demo-gcp"
	demoGCPResourceCount  = 64
	demoGCPCassetteFamily = "gcpcloud"
	demoCassetteRecording = "supply-chain-demo.json"
	demoGeneratedRoot     = "testdata/generated-cassettes"
	demoManifestRoot      = "testdata/cassettes"
)

// DemoOrgProfile is the coherent synthetic organization profile shared by the
// demo corpus and generator regeneration path. It reserves the acme GitHub
// identity scheme from scripts/verify-golden-corpus-gate.sh instead of
// introducing a second synthetic org.
type DemoOrgProfile struct {
	// Seed is the deterministic generation seed for every generated family in
	// this profile.
	Seed uint64
	// GitHubOrg is the fictional source-control organization used to synthesize
	// deterministic repository remotes.
	GitHubOrg string
	// ProjectID is the synthetic GCP project id used by the first generated
	// family.
	ProjectID string
	// ResourceCount controls the first generated GCP cassette's resource volume.
	ResourceCount int
	// CollectorLabel is the cassette collector label for the generated GCP
	// artifact.
	CollectorLabel string
	// JoinKeys is the cross-family registry of repository and package owner
	// identities reserved by this profile.
	JoinKeys JoinKeyRegistry
}

// JoinKeyRegistry stores deterministic cross-family identities that multiple
// synthetic families can share without re-deriving them differently.
type JoinKeyRegistry struct {
	repositoryRemotes map[string]string
	packageOwnerHints map[string]string
}

// GeneratedCassette is one generated cassette positioned in the checked-in
// demo-corpus family/recording layout.
type GeneratedCassette struct {
	// Family is the demo cassette family name.
	Family string
	// Recording is the cassette recording file name inside Family.
	Recording string
	// ManifestPath is the committed demo-corpus cassette path this generated
	// candidate maps to. WriteDemoOrgCassette deliberately does not write this
	// path because replacing it requires the golden-corpus answer gate.
	ManifestPath string
	// Path is the repository-relative generated artifact path used by
	// WriteDemoOrgCassette.
	Path string
	// Bytes are the canonical cassette bytes to write at Path.
	Bytes []byte
}

// DefaultDemoOrgProfile returns the coherent acme demo-org profile used by the
// first-run demo corpus and the synthetic GCP regeneration path.
func DefaultDemoOrgProfile() DemoOrgProfile {
	return DemoOrgProfile{
		Seed:           DemoOrgSeed,
		GitHubOrg:      DemoGitHubOrg,
		ProjectID:      demoGCPProjectID,
		ResourceCount:  demoGCPResourceCount,
		CollectorLabel: demoGCPCassetteFamily,
		JoinKeys:       newJoinKeyRegistry(DemoGitHubOrg),
	}
}

// Validate fails closed when the profile would not preserve the shared acme
// identity scheme.
func (p DemoOrgProfile) Validate() error {
	if p.Seed == 0 {
		return fmt.Errorf("synth/gcp: demo-org profile seed is required")
	}
	if strings.TrimSpace(p.GitHubOrg) != DemoGitHubOrg {
		return fmt.Errorf("synth/gcp: demo-org profile GitHubOrg = %q, want %q", p.GitHubOrg, DemoGitHubOrg)
	}
	if strings.TrimSpace(p.ProjectID) == "" {
		return fmt.Errorf("synth/gcp: demo-org profile ProjectID is required")
	}
	if p.ResourceCount <= 0 {
		return fmt.Errorf("synth/gcp: demo-org profile ResourceCount must be positive, got %d", p.ResourceCount)
	}
	if err := p.JoinKeys.validate(p.GitHubOrg); err != nil {
		return err
	}
	return nil
}

// Options converts the demo-org profile into the GCP generator options for the
// first generated family.
func (p DemoOrgProfile) Options() Options {
	return Options{
		Seed:           p.Seed,
		ProjectID:      p.ProjectID,
		ResourceCount:  p.ResourceCount,
		CollectorLabel: p.CollectorLabel,
		ScopeMetadata: map[string]string{
			"demo_org":          p.GitHubOrg,
			"github_org":        p.GitHubOrg,
			"identity_scheme":   DemoIdentityScheme,
			"join_key_registry": "demo-org/v1",
		},
	}
}

// GenerateDemoOrgCassette generates the GCP cassette for profile and labels it
// with the path used by the demo corpus manifest layout.
func GenerateDemoOrgCassette(profile DemoOrgProfile) (GeneratedCassette, error) {
	if err := profile.Validate(); err != nil {
		return GeneratedCassette{}, err
	}
	bytes, err := Generate(profile.Options())
	if err != nil {
		return GeneratedCassette{}, err
	}
	return GeneratedCassette{
		Family:       demoGCPCassetteFamily,
		Recording:    demoCassetteRecording,
		ManifestPath: filepath.Join(demoManifestRoot, demoGCPCassetteFamily, demoCassetteRecording),
		Path:         filepath.Join(demoGeneratedRoot, demoGCPCassetteFamily, demoCassetteRecording),
		Bytes:        bytes,
	}, nil
}

// WriteDemoOrgCassette generates the GCP demo-org cassette and writes it to the
// generated artifact path under repoRoot. It intentionally does not overwrite
// GeneratedCassette.ManifestPath; committed demo-corpus replacement must run the
// golden-corpus answer gate.
func WriteDemoOrgCassette(repoRoot string, profile DemoOrgProfile) (GeneratedCassette, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return GeneratedCassette{}, fmt.Errorf("synth/gcp: repoRoot is required")
	}
	generated, err := GenerateDemoOrgCassette(profile)
	if err != nil {
		return GeneratedCassette{}, err
	}
	outputPath := filepath.Join(repoRoot, generated.Path)
	// #nosec G301 -- generated public repo artifact path should match normal checkout directory permissions.
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return GeneratedCassette{}, fmt.Errorf("synth/gcp: create demo cassette directory: %w", err)
	}
	// #nosec G306 -- generated cassette is non-secret repository fixture data.
	if err := os.WriteFile(outputPath, generated.Bytes, 0o644); err != nil {
		return GeneratedCassette{}, fmt.Errorf("synth/gcp: write demo cassette %s: %w", generated.Path, err)
	}
	return generated, nil
}

// RepositoryRemote returns the deterministic GitHub remote for repo.
func (r JoinKeyRegistry) RepositoryRemote(repo string) (string, bool) {
	remote, ok := r.repositoryRemotes[repo]
	return remote, ok
}

// PackageOwnerHint returns the deterministic owner repository remote for a
// package-registry package id.
func (r JoinKeyRegistry) PackageOwnerHint(packageID string) (string, bool) {
	remote, ok := r.packageOwnerHints[packageID]
	return remote, ok
}

func newJoinKeyRegistry(org string) JoinKeyRegistry {
	remotes := make(map[string]string, len(demoCorpusRepositories))
	for _, repo := range demoCorpusRepositories {
		remotes[repo] = fmt.Sprintf("github.com/%s/%s", org, repo)
	}
	return JoinKeyRegistry{
		repositoryRemotes: remotes,
		packageOwnerHints: map[string]string{
			fmt.Sprintf("github.com/%s/lib-common", org): remotes["lib-common"],
		},
	}
}

func (r JoinKeyRegistry) validate(org string) error {
	if len(r.repositoryRemotes) == 0 {
		return fmt.Errorf("synth/gcp: demo-org join-key registry has no repository remotes")
	}
	prefix := fmt.Sprintf("github.com/%s/", org)
	for _, repo := range []string{"api-svc", "orders-api", "lib-common"} {
		remote, ok := r.RepositoryRemote(repo)
		if !ok {
			return fmt.Errorf("synth/gcp: demo-org join-key registry missing repository %q", repo)
		}
		if remote != prefix+repo {
			return fmt.Errorf("synth/gcp: demo-org repository %q remote = %q, want %q", repo, remote, prefix+repo)
		}
	}
	owner, ok := r.PackageOwnerHint(prefix + "lib-common")
	if !ok {
		return fmt.Errorf("synth/gcp: demo-org join-key registry missing package owner hint for %s", prefix+"lib-common")
	}
	if owner != prefix+"lib-common" {
		return fmt.Errorf("synth/gcp: demo-org package owner hint = %q, want %q", owner, prefix+"lib-common")
	}
	return nil
}

var demoCorpusRepositories = []string{
	"go_comprehensive",
	"python_comprehensive",
	"terraform_comprehensive",
	"terragrunt_comprehensive",
	"kubernetes_comprehensive",
	"helm_argocd_platform",
	"lib-common",
	"orders-api",
	"deployable-source",
	"deployable-config",
	"kustomize-deployable-overlay",
	"ansible-platform-playbooks",
	"ansible-shared-roles",
	"jenkins-ci-pipelines",
	"puppet-platform-modules",
	"chef-cookbooks",
	"salt-formulas",
	"helm-umbrella-chart",
	"helm-template-chart",
	"api-svc",
}
