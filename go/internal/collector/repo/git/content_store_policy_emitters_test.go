// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq/eshu

package git

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const emitterSeed = "emitterseedcredential0123456789"

// emitterFixtures holds one file per body-reading emitter in streamFacts. Every
// body carries emitterSeed where that emitter copies text into a fact.
var emitterFixtures = map[string]string{
	// Service-catalog emitter (Backstage).
	"catalog-info.yaml": "apiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n  name: payments\nspec:\n  type: service\n  lifecycle: production\n  owner: " + emitterSeed + "\n",
	// Workflow-image emitter.
	".github/workflows/deploy.yml": "name: deploy\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - name: Push\n        run: docker push registry.example.com/team/" + emitterSeed + ":prod\n",
	// Documentation emitter, reached through the content-file branch because
	// the governed registry binds the name to a definition.
	"guide.md": "# Guide\n\nDeploy notes.\n\ntoken: " + emitterSeed + "\n",
	// CODEOWNERS and .gitmodules candidates, diverted before the parser.
	"CODEOWNERS":  "*.go @org/" + emitterSeed + "\n",
	".gitmodules": "[submodule \"lib\"]\n\tpath = lib\n\turl = https://example.com/org/" + emitterSeed + ".git\n",
}

// emitterPolicyRegistry binds policy to the yaml and raw_text definitions and
// adds the exact names the diverted and documentation fixtures need.
func emitterPolicyRegistry(t *testing.T, store parser.ContentStore) parser.Registry {
	t.Helper()
	definitions := parser.DefaultRegistry().Definitions()
	for i := range definitions {
		switch definitions[i].ParserKey {
		case "yaml", "raw_text":
			definitions[i].ContentStore = store
		}
		if definitions[i].ParserKey == "raw_text" {
			definitions[i].ExactNames = append(definitions[i].ExactNames, "CODEOWNERS", ".gitmodules", "guide.md")
		}
	}
	registry, err := parser.NewRegistry(definitions)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

func snapshotFixtureRepo(t *testing.T, registry parser.Registry, files map[string]string) []facts.Envelope {
	t.Helper()
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	for path, body := range files {
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(path)), body)
	}
	resolved, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolved = repoRoot
	}
	engine, err := parser.NewEngine(registry, parser.NewRuntime())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine, Registry: registry}).SnapshotRepository(
		context.Background(), SelectedRepository{RepoPath: resolved})
	if err != nil {
		t.Fatalf("SnapshotRepository: %v", err)
	}
	observedAt, _ := time.Parse(time.RFC3339, policyObservedAt)
	collected := buildStreamingGeneration(resolved, repositoryidentity.Metadata{ID: "repo://example/emitters", Name: "emitters"},
		"run", observedAt, snapshot, false, "")
	return drainFactChannel(collected.Facts)
}

// factKindsWithSeed returns the sorted fact kinds whose payload holds seed.
func factKindsWithSeed(t *testing.T, envelopes []facts.Envelope, seed string) []string {
	t.Helper()
	found := map[string]bool{}
	for _, envelope := range envelopes {
		raw, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatalf("marshal %s payload: %v", envelope.FactKind, err)
		}
		if strings.Contains(string(raw), seed) {
			found[envelope.FactKind] = true
		}
	}
	kinds := make([]string, 0, len(found))
	for kind := range found {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// TestEveryBodyReadingEmitterSeesTheStoredBody scans EVERY emitted fact
// payload (not only content and content_entity) for a seeded credential. The
// control run proves each emitter copies the seed into a fact when the policy
// is store; the metadata and redact runs prove none does once a policy binds.
func TestEveryBodyReadingEmitterSeesTheStoredBody(t *testing.T) {
	t.Parallel()

	control := factKindsWithSeed(t, snapshotFixtureRepo(t, parser.DefaultRegistry(), emitterFixtures), emitterSeed)
	// Each emitter must copy the seed in the control run, or its fixture proves
	// nothing under a policy.
	for _, kind := range []string{
		"content", "ci.workflow_image_evidence", "codeowners.ownership",
		"documentation_section", "service_catalog.ownership", "submodule.pin",
	} {
		if !slices.Contains(control, kind) {
			t.Fatalf("control: fact kind %q does not hold the seed (got %v); its fixture is vacuous", kind, control)
		}
	}

	cases := []struct {
		name  string
		store parser.ContentStore
		skip  []string
	}{
		{name: "metadata", store: parser.ContentStore{Policy: parser.ContentStoreMetadata}},
		{
			name: "redact",
			store: parser.ContentStore{
				Policy: parser.ContentStoreRedact, Redactor: parser.RedactorMarker,
				CredentialKeys: []string{"owner", "run", "url"},
			},
			// Marker redaction is key- and detector-based. A CODEOWNERS line
			// has no key and no detector shape, so only metadata can hide it.
			skip: []string{"CODEOWNERS"},
		},
	}
	for _, tc := range cases {
		files := map[string]string{}
		for path, body := range emitterFixtures {
			if !slices.Contains(tc.skip, path) {
				files[path] = body
			}
		}
		leaked := factKindsWithSeed(t, snapshotFixtureRepo(t, emitterPolicyRegistry(t, tc.store), files), emitterSeed)
		if len(leaked) != 0 {
			t.Errorf("%s: the seeded credential reached fact kinds %v", tc.name, leaked)
		}
	}
}

// TestDeltaOnlyBodyReadersApplyThePolicy drives the two streamFacts loops that
// only a delta generation fills: workflow files re-read for image evidence and
// documentation files re-read for documentation facts. The control run proves
// each loop copies the seed when no policy is set.
func TestDeltaOnlyBodyReadersApplyThePolicy(t *testing.T) {
	t.Parallel()

	run := func(stores map[string]parser.ContentStore) []facts.Envelope {
		repoPath := t.TempDir()
		for _, path := range []string{".github/workflows/deploy.yml", "guide.md"} {
			writeCollectorTestFile(t, filepath.Join(repoPath, filepath.FromSlash(path)), emitterFixtures[path])
		}
		snapshot := RepositorySnapshot{
			Delta:                  true,
			WorkflowImageFileMetas: []model.ContentFileMeta{{RelativePath: ".github/workflows/deploy.yml", CommitSHA: "c0ffee"}},
			DocumentationFileMetas: []model.ContentFileMeta{{RelativePath: "guide.md", CommitSHA: "c0ffee"}},
			ContentStores:          stores,
		}
		observedAt, _ := time.Parse(time.RFC3339, policyObservedAt)
		collected := buildStreamingGeneration(repoPath, repositoryidentity.Metadata{ID: "repo://example/delta", Name: "delta"},
			"run", observedAt, snapshot, false, "")
		return drainFactChannel(collected.Facts)
	}

	control := factKindsWithSeed(t, run(nil), emitterSeed)
	for _, kind := range []string{"ci.workflow_image_evidence", "documentation_section"} {
		if !slices.Contains(control, kind) {
			t.Fatalf("control: %q does not hold the seed (got %v); the loop is not exercised", kind, control)
		}
	}
	metadata := parser.ContentStore{Policy: parser.ContentStoreMetadata}
	redact := parser.ContentStore{Policy: parser.ContentStoreRedact, Redactor: parser.RedactorMarker, CredentialKeys: []string{"run"}}
	for name, store := range map[string]parser.ContentStore{"metadata": metadata, "redact": redact} {
		leaked := factKindsWithSeed(t, run(map[string]parser.ContentStore{
			".github/workflows/deploy.yml": store, "guide.md": store,
		}), emitterSeed)
		if len(leaked) != 0 {
			t.Errorf("%s: the seeded credential reached fact kinds %v", name, leaked)
		}
	}
}
