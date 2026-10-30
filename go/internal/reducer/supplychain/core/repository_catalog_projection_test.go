// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	payloadstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
)

// The #9567 differential: the real container_image_identity and
// supply_chain_impact handlers produce byte-identical decisions and findings
// whether the repository catalog carries the full payload or the projected
// one the store now returns. The fixture is the QA-shaped shim (#9567) scaled
// down: catalog repositories with imports_map, git_refs and delta arrays, OCI
// manifests whose source label names a catalog repository, and npm CVE
// intents with manifest-dependency evidence.
const (
	catalogDiffRepos      = 60
	catalogDiffCIIIntents = 20
	catalogDiffSCIIntents = 20
)

func catalogDiffRepoID(i int) string { return fmt.Sprintf("repository:r_%06d", i) }
func catalogDiffRemote(i int) string { return fmt.Sprintf("https://github.com/acme/service-%03d", i) }
func catalogDiffPkg(i int) string    { return fmt.Sprintf("pkg-%03d", i) }

// catalogDiffRepositories returns the catalog as the store would decode it,
// round-tripping each payload through JSON and the production payload decoder.
func catalogDiffRepositories(t *testing.T, projected bool) []facts.Envelope {
	t.Helper()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	out := make([]facts.Envelope, 0, catalogDiffRepos)
	for i := 0; i < catalogDiffRepos; i++ {
		id := catalogDiffRepoID(i)
		imports := map[string][]string{}
		for s := 0; s < 30; s++ {
			imports[fmt.Sprintf("pkg%03d.module_%04d.Symbol", i, s)] = []string{
				fmt.Sprintf("src/service_%03d/module_%04d/a.py", i, s),
				fmt.Sprintf("src/service_%03d/module_%04d/b.py", i, s),
			}
		}
		payload := map[string]any{
			"graph_id": id, "graph_kind": "repository", "name": fmt.Sprintf("service-%03d", i),
			"repo_id": id, "repo_slug": fmt.Sprintf("acme/service-%03d", i), "remote_url": catalogDiffRemote(i),
			"local_path": fmt.Sprintf("/data/repos/acme/service-%03d", i), "default_branch": "main",
			"imports_map":                  imports,
			"git_refs":                     []map[string]any{{"name": "main", "head_sha": "0123456789abcdef0123456789abcdef01234567", "is_default": true}},
			"delta_relative_paths":         []string{"src/a.go"},
			"delta_deleted_relative_paths": []string{"src/b.go"},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal repository payload: %v", err)
		}
		decoded, err := payloadstore.UnmarshalPayload(raw)
		if err != nil {
			t.Fatalf("decode repository payload: %v", err)
		}
		if projected {
			decoded = facts.ProjectRepositoryCatalogPayload(decoded)
		}
		out = append(out, facts.Envelope{
			FactID: "repository:" + id, ScopeID: "git-repository-scope:" + id, GenerationID: "gen-" + id,
			FactKind: "repository", SchemaVersion: "1.0.0", CollectorKind: "git", FencingToken: 1,
			SourceConfidence: facts.SourceConfidenceObserved, ObservedAt: base.Add(time.Duration(i) * time.Second),
			Payload: decoded,
		})
	}
	return out
}

// catalogDiffLoader serves one fixed catalog plus the cross-scope OCI and
// manifest-dependency evidence both handlers read.
type catalogDiffLoader struct {
	repositories []facts.Envelope
	scopeFacts   map[string][]facts.Envelope
	ociActive    []facts.Envelope
	dependencies []facts.Envelope
}

func (l *catalogDiffLoader) ListFacts(_ context.Context, scopeID, _ string) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts[scopeID]...), nil
}

func (l *catalogDiffLoader) ListFactsByKind(_ context.Context, scopeID, _ string, _ []string) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts[scopeID]...), nil
}

func (l *catalogDiffLoader) ListActiveContainerImageIdentityFacts(context.Context) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.ociActive...), nil
}

func (l *catalogDiffLoader) ListActiveContainerImageIdentityWarnings(context.Context) ([]facts.Envelope, error) {
	return nil, nil
}

func (l *catalogDiffLoader) ListActiveContainerImageSLSAFacts(context.Context) ([]facts.Envelope, error) {
	return nil, nil
}

func (l *catalogDiffLoader) ListActiveContainerImageCIFacts(context.Context, string) ([]facts.Envelope, error) {
	return nil, nil
}

func (l *catalogDiffLoader) ListActiveSupplyChainImpactFacts(context.Context, SupplyChainImpactFactFilter) ([]facts.Envelope, bool, error) {
	return nil, false, nil
}

func (l *catalogDiffLoader) ListActivePackageManifestDependencyFacts(context.Context, []string, []string) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.dependencies...), nil
}

func (l *catalogDiffLoader) ListActiveRepositoryFacts(context.Context) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.repositories...), nil
}

type catalogDiffOutputs map[string]string

func (o catalogDiffOutputs) put(t *testing.T, id string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal output %s: %v", id, err)
	}
	sum := sha256.Sum256(raw)
	o[id] = hex.EncodeToString(sum[:])
}

type catalogDiffCIIWriter struct {
	t   *testing.T
	out catalogDiffOutputs
}

func (*catalogDiffCIIWriter) ContainerImageIdentityActivationEpoch(context.Context, string, string) (int64, error) {
	return 1, nil
}

func (w *catalogDiffCIIWriter) WriteContainerImageIdentityDecisions(_ context.Context, write containerimage.ContainerImageIdentityWrite) (containerimage.ContainerImageIdentityWriteResult, error) {
	w.out.put(w.t, write.IntentID, write.Decisions)
	return containerimage.ContainerImageIdentityWriteResult{CanonicalWrites: len(write.Decisions)}, nil
}

type catalogDiffSCIWriter struct {
	t   *testing.T
	out catalogDiffOutputs
}

func (w *catalogDiffSCIWriter) WriteSupplyChainImpactFindings(_ context.Context, write SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	w.out.put(w.t, write.IntentID, write.Findings)
	w.out["#findings"] = fmt.Sprint(len(write.Findings) + catalogDiffAtoi(w.out["#findings"]))
	return SupplyChainImpactWriteResult{CanonicalWrites: len(write.Findings), FactsWritten: len(write.Findings)}, nil
}

func catalogDiffAtoi(value string) int {
	var n int
	_, _ = fmt.Sscan(value, &n)
	return n
}

// runCatalogDiff runs every intent through the real handlers against the given
// catalog and returns per-intent output digests.
func runCatalogDiff(t *testing.T, repositories []facts.Envelope) catalogDiffOutputs {
	t.Helper()
	fixedNow := func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	loader := &catalogDiffLoader{repositories: repositories, scopeFacts: map[string][]facts.Envelope{}}
	for i := 0; i < catalogDiffCIIIntents; i++ {
		loader.ociActive = append(loader.ociActive, facts.Envelope{
			FactID: fmt.Sprintf("oci-manifest-%d", i), FactKind: chain.OCIImageManifestFactKind,
			ScopeID: "oci-registry://registry.example.com/acme", GenerationID: "gen-oci",
			SourceConfidence: facts.SourceConfidenceObserved,
			Payload: map[string]any{
				"registry": "registry.example.com", "repository": fmt.Sprintf("acme/service-%03d", i*2),
				"repository_id": fmt.Sprintf("oci-registry://registry.example.com/acme/service-%03d", i*2),
				"digest":        fmt.Sprintf("sha256:%064x", i+1), "media_type": "application/vnd.oci.image.manifest.v1+json",
				"config_labels": map[string]any{"org.opencontainers.image.source": catalogDiffRemote(i * 2)},
			},
		})
	}
	for j := 0; j < catalogDiffSCIIntents*2; j++ {
		loader.dependencies = append(loader.dependencies, packageManifestDependencyFactWithMetadata(
			catalogDiffRepoID(j%catalogDiffRepos), fmt.Sprintf("service-%03d", j%catalogDiffRepos),
			"package-lock.json", catalogDiffPkg(j%catalogDiffSCIIntents), "npm", "1.0.0", fixedNow(),
			map[string]any{"section": "package-lock", "lockfile": true, "direct_dependency": true}))
	}

	outputs := catalogDiffOutputs{}
	cii := containerimage.ContainerImageIdentityHandler{FactLoader: loader, Writer: &catalogDiffCIIWriter{t: t, out: outputs}, Now: fixedNow}
	sci := SupplyChainImpactHandler{
		FactLoader: loader, Writer: &catalogDiffSCIWriter{t: t, out: outputs},
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(), Now: fixedNow,
	}
	ctx := context.Background()
	for i := 0; i < catalogDiffCIIIntents; i++ {
		intent := reducercontract.Intent{
			IntentID:     fmt.Sprintf("cii-%03d", i),
			ScopeID:      fmt.Sprintf("oci-registry://registry.example.com/acme/service-%03d", i*2),
			GenerationID: fmt.Sprintf("gen-cii-%03d", i), SourceSystem: "oci_registry",
			Domain: reducercontract.DomainContainerImageIdentity, Cause: "catalog-diff",
		}
		if _, err := cii.Handle(ctx, intent); err != nil {
			t.Fatalf("container_image_identity %s: %v", intent.IntentID, err)
		}
	}
	for i := 0; i < catalogDiffSCIIntents; i++ {
		name := catalogDiffPkg(i)
		scope := fmt.Sprintf("vuln-intel://osv/npm/%s@1.0.0", name)
		cve := fmt.Sprintf("CVE-2026-%05d", i)
		loader.scopeFacts[scope] = []facts.Envelope{
			vulnerabilityCVEFact("cve-"+name, cve, 7.5),
			vulnerabilityAffectedPackageFact("affected-"+name, cve, "npm://registry.npmjs.org/"+name, "npm", name, "1.0.0", "1.0.1"),
		}
		intent := reducercontract.Intent{
			IntentID: fmt.Sprintf("sci-%03d", i), ScopeID: scope,
			GenerationID: fmt.Sprintf("gen-sci-%03d", i), SourceSystem: "vulnerability_intelligence",
			Domain: reducercontract.DomainSupplyChainImpact, Cause: "catalog-diff",
		}
		if _, err := sci.Handle(ctx, intent); err != nil {
			t.Fatalf("supply_chain_impact %s: %v", intent.IntentID, err)
		}
	}
	return outputs
}

func TestRepositoryCatalogProjectionKeepsHandlerOutputsIdentical(t *testing.T) {
	t.Parallel()

	full := catalogDiffRepositories(t, false)
	if _, ok := full[0].Payload["imports_map"]; !ok {
		t.Fatal("full catalog fixture lost imports_map; the differential would be vacuous")
	}
	projected := catalogDiffRepositories(t, true)
	for _, key := range facts.RepositoryCatalogOmittedPayloadKeys() {
		if _, ok := projected[0].Payload[key]; ok {
			t.Fatalf("projected catalog fixture still carries %q", key)
		}
	}

	before := runCatalogDiff(t, full)
	after := runCatalogDiff(t, projected)
	if catalogDiffAtoi(before["#findings"]) == 0 {
		t.Fatal("supply_chain_impact wrote no findings; its half of the differential is vacuous")
	}
	if got, want := len(before), catalogDiffCIIIntents+catalogDiffSCIIntents+1; got != want {
		t.Fatalf("baseline wrote %d intent outputs, want %d", got, want)
	}
	if len(after) != len(before) {
		t.Fatalf("projected run wrote %d outputs, baseline %d", len(after), len(before))
	}
	for id, digest := range before {
		if after[id] != digest {
			t.Fatalf("intent %s output changed under the projected catalog: %s -> %s", id, digest, after[id])
		}
	}

	// Non-vacuity: container_image_identity decisions depend on the catalog,
	// so an empty catalog must change them. Without this the identical
	// digests could come from handlers that never read the catalog.
	empty := runCatalogDiff(t, nil)
	changed := 0
	for i := 0; i < catalogDiffCIIIntents; i++ {
		id := fmt.Sprintf("cii-%03d", i)
		if empty[id] != before[id] {
			changed++
		}
	}
	if changed == 0 {
		t.Fatal("container_image_identity outputs ignore the catalog; the differential proves nothing")
	}
	t.Logf("%d intents identical under projection (%s supply_chain_impact findings); %d/%d container_image_identity outputs depend on the catalog",
		len(before)-1, before["#findings"], changed, catalogDiffCIIIntents)
}
