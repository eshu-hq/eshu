// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
)

// fakeCounter satisfies graphCounter from maps keyed by identifier.
type fakeCounter struct {
	nodes map[string]int64
	edges map[string]int64
	corr  map[string]int64 // key "from|rel|to"
	// corrEv keys "from|rel|to|kind1,kind2" for evidence-filtered correlation
	// counts. A miss returns 0, modelling a shared edge that exists (corr > 0)
	// but carries no edge produced by the requested verb's evidence kind.
	corrEv map[string]int64
	// edgeProp keys "from|rel|to|kind1,kind2|prop" -> the property value of each
	// matching (evidence-narrowed) edge ("" = absent). nodeProp keys "label|prop".
	edgeProp map[string][]string
	nodeProp map[string][]string
	// selfLoop keys "label|relationship|property|value" -> self-loop edge count.
	selfLoop map[string]int64
	// elements is every node and edge with its property map.
	elements []gg.GraphElementProperties
	err      error
}

func (f fakeCounter) CountNodes(_ context.Context, label string) (int64, error) {
	return f.nodes[label], f.err
}

func (f fakeCounter) CountEdges(_ context.Context, rel string) (int64, error) {
	return f.edges[rel], f.err
}

func (f fakeCounter) CountCorrelation(_ context.Context, from, rel, to string) (int64, error) {
	return f.corr[from+"|"+rel+"|"+to], f.err
}

func (f fakeCounter) CountCorrelationWithEvidence(_ context.Context, from, rel, to string, kinds []string) (int64, error) {
	return f.corrEv[from+"|"+rel+"|"+to+"|"+strings.Join(kinds, ",")], f.err
}

func (f fakeCounter) ListCorrelationEdgeProperty(_ context.Context, from, rel, to string, kinds []string, prop string) ([]string, error) {
	return f.edgeProp[from+"|"+rel+"|"+to+"|"+strings.Join(kinds, ",")+"|"+prop], f.err
}

func (f fakeCounter) ListNodeProperty(_ context.Context, label, prop string) ([]string, error) {
	return f.nodeProp[label+"|"+prop], f.err
}

func (f fakeCounter) CountSelfLoopEdges(_ context.Context, label, relationship, property, value string) (int64, error) {
	return f.selfLoop[label+"|"+relationship+"|"+property+"|"+value], f.err
}

// dartSelfLoopFloor seeds the unconditionally-asserted required_self_loops
// exact bound (sl-dart-calls-recursion, issue #5349) so a minimal-gate test can
// satisfy the snapshot's required self-loops while focusing on its own
// assertion. The pinned count of 2 mirrors tests/fixtures/ecosystems/
// dart_comprehensive/calls.dart's recursionFib + recursionFact self-calls (see
// testdata/golden/e2e-20repo-snapshot.json).
func dartSelfLoopFloor() map[string]int64 {
	return map[string]int64{"Function|CALLS|language|dart": 2}
}

// fileLanguageFloor seeds every unconditionally-asserted required_nodes floor
// (rn-file-language, rn-dataplex-entry-group, rn-identity-platform-config,
// rn-flux-kustomization-source-ref, rn-flux-git-repository-url,
// rn-flux-oci-repository-url, rn-flux-bucket-name, rn-flux-helm-release-
// source-ref, rn-flux-helm-repository-url,
// rn-terraform-resource-attribute-promotion,
// rn-terraform-state-provider-binding, rn-codeowner-team-ref,
// rn-cloud-resource-running-image, rn-ec2-instance-identity-ami,
// rn-ec2-ami-node) so a
// minimal-gate test can satisfy the snapshot's required nodes while focusing
// on its own assertion. The two GCP posture-only entries pin identity via a
// single CloudResource node carrying the matching resource_type value; the
// four Flux PR A entries (issue #5360 PR A) pin identity via a
// FluxKustomization node carrying source_ref_kind, a FluxGitRepository node
// carrying url, a FluxOCIRepository node carrying url, and a FluxBucket node
// carrying bucket_name; the two Flux Helm entries (issue #5483 C1) pin
// identity via a FluxHelmRelease node carrying source_ref_kind and a
// FluxHelmRepository node carrying url; the #5441 entry pins identity via a
// TerraformStateResource node (renamed from TerraformResource by #5443)
// carrying tf_attr_instance_type; the #5446 entry pins identity via the SAME
// TerraformStateResource node additionally carrying provider="aws";
// CodeownerTeam/ref (#5419 Phase 5); the #5450 entry pins identity via
// CloudResource nodes carrying running_image_ref and running_image_digest (the
// ECS running task's and Lambda function's deployed image evidence); and the
// #5448 entry pins identity via a CloudResource node carrying ami_id (the EC2
// instance identity materialization's disjoint augment property); and the
// #5717 entry pins identity via a CloudResource node carrying resource_type
// aws_ec2_ami (the AMI node class that lets the #5448 relationship resolve);
// see testdata/golden/e2e-20repo-snapshot.json.
func fileLanguageFloor() (map[string]int64, map[string][]string) {
	langs := make([]string, 10)
	for i := range langs {
		langs[i] = "go"
	}
	nodes := map[string]int64{
		"File":                   int64(len(langs)),
		"CloudResource":          2,
		"FluxKustomization":      1,
		"FluxGitRepository":      1,
		"FluxOCIRepository":      1,
		"FluxBucket":             1,
		"FluxHelmRelease":        1,
		"FluxHelmRepository":     1,
		"TerraformStateResource": 1,
		"CodeownerTeam":          1,
		"PackageArtifact":        1,
		"RegistryEvent":          1,
	}
	nodeProp := map[string][]string{
		"File|language": langs,
		// rn-package-artifact-hashes (#5820 P2 review finding): mirrors the
		// package_registry supply-chain-demo cassette's
		// github.com/acme/lib-common@1.0.0 package_artifact fact hashes, "|"
		// joined the way boltPropertyString joins a live Bolt list property.
		"PackageArtifact|hashes": {
			"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855|" +
				"sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
		},
		// rn-registry-event-fields: mirrors the package_registry
		// supply-chain-demo cassette's github.com/acme/lib-common@1.0.0
		// registry_event fact's event_type (event_key serial:9988), the same
		// non-vacuity precedent as PackageArtifact|hashes above.
		"RegistryEvent|event_type": {"yank"},
		"CloudResource|resource_type": {
			"dataplex.googleapis.com/EntryGroup",
			"identitytoolkit.googleapis.com/Config",
			// rn-ec2-ami-node (#5717): the AMI's own aws_resource identity fact
			// materializes as an ordinary CloudResource node under this
			// resource_type via the generic AWS resource node materialization
			// path.
			"aws_ec2_ami",
		},
		"CloudResource|ami_id":                         {"ami-000000000000000a"},
		"FluxKustomization|source_ref_kind":            {"GitRepository"},
		"FluxGitRepository|url":                        {"https://github.com/acme/flux-system"},
		"FluxOCIRepository|url":                        {"oci://ghcr.io/acme/app-manifests"},
		"FluxBucket|bucket_name":                       {"flux-artifacts"},
		"FluxHelmRelease|source_ref_kind":              {"HelmRepository"},
		"FluxHelmRepository|url":                       {"https://stefanprodan.github.io/podinfo"},
		"TerraformStateResource|tf_attr_instance_type": {"t3.micro"},
		"TerraformStateResource|provider":              {"aws"},
		"CodeownerTeam|ref":                            {"@eshu-hq/platform"},
		"CloudResource|running_image_ref": {
			"123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
			"123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
		},
		// running_image_digest carries the BARE digest for both ECS and Lambda
		// (issue #5450 P2 fix): the full registry/repository@digest reference
		// is available via running_image_ref, and running_image_digest is
		// normalized to "sha256:<hex>" for both resource types so a consumer
		// never has to branch on resource_type to know which shape it is
		// getting.
		"CloudResource|running_image_digest": {
			// #5452: the ECS running task now runs the SCANNED vulnerable digest
			// (...901a), so its supply_chain_impact finding classifies
			// runtime_confirmed via the query-time CloudResource probe.
			"sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			"sha256:0000000000000000000000000000000000000000000000000000000000aaaacc",
		},
	}
	return nodes, nodeProp
}

func TestCheckGraphRequiredOnlyPassesOnExistence(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	nodes, nodeProp := fileLanguageFloor()
	c := fakeCounter{
		corr: map[string]int64{
			"Repository|CORRELATES_DEPLOYABLE_UNIT|Repository": 2,
			"Function|RUNS_IN|Workload":                        1,
			"Repository|DEPENDS_ON|Repository":                 7,
			"KubernetesWorkload|RUNS_IMAGE|OciImageManifest":   1,
		},
		nodes:    nodes,
		nodeProp: nodeProp,
		selfLoop: dartSelfLoopFloor(),
	}
	var r Report
	if err := checkGraph(context.Background(), c, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass; findings: %+v", r.Findings)
	}
}

func TestCheckGraphAdvisoryCorrelationDoesNotBlock(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	// rc-1 and rc-3 present; rc-2 and rc-4 absent but advisory (not in blocking set).
	nodes, nodeProp := fileLanguageFloor()
	c := fakeCounter{
		corr: map[string]int64{
			"Repository|CORRELATES_DEPLOYABLE_UNIT|Repository": 1,
			"Repository|DEPENDS_ON|Repository":                 1,
		},
		nodes:    nodes,
		nodeProp: nodeProp,
		selfLoop: dartSelfLoopFloor(),
	}
	var r Report
	if err := checkGraph(context.Background(), c, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("advisory rc-2/rc-4 absence must not fail the minimal gate; findings: %+v", r.Findings)
	}
}

func TestCheckGraphRequiredFailsWhenCorrelationMissing(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := checkGraph(context.Background(), fakeCounter{}, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("expected failure when blocking correlations are absent")
	}
}

func TestSplitCSVTrimsAndDropsEmpty(t *testing.T) {
	got := splitCSV("rc-1, rc-3 ,, code_calls")
	want := []string{"rc-1", "rc-3", "code_calls"}
	if len(got) != len(want) {
		t.Fatalf("splitCSV = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitCSV[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if len(splitCSV("")) != 0 || len(splitCSV("  ,  ")) != 0 {
		t.Error("empty / whitespace-only input must yield no elements")
	}
}

func TestPhaseSet(t *testing.T) {
	if s := phaseSet("all"); !s["drains"] || !s["graph"] || !s["query"] || !s["timing"] {
		t.Errorf("all => %+v", s)
	}
	s := phaseSet("drains,graph")
	if !s["drains"] || !s["graph"] || s["query"] || s["timing"] {
		t.Errorf("subset => %+v", s)
	}
}
