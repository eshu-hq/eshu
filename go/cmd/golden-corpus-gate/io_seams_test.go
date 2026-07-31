// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
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
			"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85|" +
				"sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3",
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
			"sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
			"sha256:0000000000000000000000000000000000000000000000000000000000cc",
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
	if err := checkGraph(context.Background(), c, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, &r); err != nil {
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
	if err := checkGraph(context.Background(), c, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, &r); err != nil {
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
	if err := checkGraph(context.Background(), fakeCounter{}, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("expected failure when blocking correlations are absent")
	}
}

func TestQueryClientChecksHTTPShapes(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	shapesByPath := make(map[string]QueryShape, len(snap.QueryShapes.HTTP))
	for key, shape := range snap.QueryShapes.HTTP {
		_, path, err := parseHTTPShapeKey(key)
		if err != nil {
			t.Fatal(err)
		}
		shapesByPath[path] = shape
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		shape, ok := shapesByPath[req.URL.RequestURI()]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fakeQueryShapeResponse(shape)); err != nil {
			t.Errorf("write query shape response: %v", err)
		}
	}))
	defer srv.Close()

	client := newQueryClient(srv.URL, "k")
	var r Report
	if err := checkQuery(context.Background(), client, snap, &r); err != nil {
		t.Fatalf("checkQuery err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected query shapes to pass; findings: %+v", r.Findings)
	}
}

func TestQueryClientChecksPostEnvelopeShapes(t *testing.T) {
	snap := Snapshot{QueryShapes: QueryShapes{HTTP: map[string]QueryShape{
		"POST /api/v0/code/dead-code/cross-repo": {
			Envelope:               true,
			RequestBody:            map[string]any{"repo_id": "deadcode-producer", "language": "go", "limit": float64(20)},
			RequiredResponseFields: []string{"data", "truth", "error"},
			RequiredJSONPaths: []string{
				"data.candidate_buckets.live_by_consumer[].consumer_evidence[].citation",
			},
			RequiredJSONValues: map[string]any{
				"truth.level":      "derived",
				"truth.basis":      "hybrid",
				"data.query_shape": "bounded_cross_repo_dead_code",
			},
		},
	}}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.Method)
		}
		if got, want := req.Header.Get("Accept"), EnvelopeMIMEType; got != want {
			t.Errorf("Accept = %q, want %q", got, want)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["repo_id"] != "deadcode-producer" || body["language"] != "go" {
			t.Fatalf("request body = %#v, want deadcode-producer go selector", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "data": {
		    "query_shape": "bounded_cross_repo_dead_code",
		    "candidate_buckets": {
		      "live_by_consumer": [{
		        "consumer_evidence": [{"citation": "code_reachability_rows:scope/gen/consumer/root/entity"}]
		      }]
		    }
		  },
		  "truth": {"level": "derived", "basis": "hybrid"},
		  "error": null
		}`))
	}))
	defer srv.Close()

	var r Report
	if err := checkQuery(context.Background(), newQueryClient(srv.URL, ""), snap, &r); err != nil {
		t.Fatalf("checkQuery err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected POST envelope query shape to pass; findings: %+v", r.Findings)
	}
}

func fakeQueryShapeResponse(shape QueryShape) map[string]any {
	resp := make(map[string]any, len(shape.RequiredResponseFields))
	// ResultsField names the array the shape actually asserts against
	// (eshu-hq/eshu#5566); this generator must populate that exact field as
	// an array, not just the first required_response_fields entry, or a
	// shape whose asserted collection is not the first-listed field would
	// get a fixture that does not match what EvaluateQueryShape checks.
	arrayField := shape.ResultsField
	for _, field := range shape.RequiredResponseFields {
		if arrayField != "" && field == arrayField {
			count := max(shape.MinimumResults, 1)
			items := make([]map[string]any, count)
			for i := range items {
				item := make(map[string]any, len(shape.ResultItemRequiredFields))
				for _, itemField := range shape.ResultItemRequiredFields {
					item[itemField] = "value"
				}
				items[i] = item
			}
			resp[field] = items
			continue
		}
		resp[field] = map[string]any{}
	}
	for _, path := range shape.RequiredJSONPaths {
		fakeSetJSONPath(resp, path, "value")
	}
	objectMatchPaths := make([]string, 0, len(shape.RequiredJSONObjectMatches))
	for path := range shape.RequiredJSONObjectMatches {
		objectMatchPaths = append(objectMatchPaths, path)
	}
	sort.Slice(objectMatchPaths, func(i, j int) bool {
		leftDepth := strings.Count(objectMatchPaths[i], ".")
		rightDepth := strings.Count(objectMatchPaths[j], ".")
		if leftDepth == rightDepth {
			return objectMatchPaths[i] < objectMatchPaths[j]
		}
		return leftDepth < rightDepth
	})
	for _, path := range objectMatchPaths {
		matches := shape.RequiredJSONObjectMatches[path]
		fakeSetJSONObjectMatches(resp, path, matches)
	}
	for path, value := range shape.RequiredJSONValues {
		fakeSetJSONPath(resp, path, value)
	}
	return resp
}

func fakeSetJSONPath(root map[string]any, path string, value any) {
	segments := strings.Split(path, ".")
	var current any = root
	for i, rawSegment := range segments {
		last := i == len(segments)-1
		arraySegment := strings.HasSuffix(rawSegment, "[]")
		segment := strings.TrimSuffix(rawSegment, "[]")
		obj, ok := current.(map[string]any)
		if !ok || segment == "" {
			return
		}
		if arraySegment {
			if last {
				if arr, ok := obj[segment].([]any); ok {
					obj[segment] = append(arr, value)
				} else {
					obj[segment] = []any{value}
				}
				return
			}
			switch arr := obj[segment].(type) {
			case []map[string]any:
				if len(arr) > 0 {
					current = arr[0]
					continue
				}
			case []any:
				if len(arr) > 0 {
					current = arr[0]
					continue
				}
			}
			arr := []any{map[string]any{}}
			obj[segment] = arr
			current = arr[0]
			continue
		}
		if last {
			obj[segment] = value
			return
		}
		next, _ := obj[segment].(map[string]any)
		if next == nil {
			next = map[string]any{}
			obj[segment] = next
		}
		current = next
	}
}

func TestQueryClientFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := checkQuery(context.Background(), newQueryClient(srv.URL, ""), snap, &r); err != nil {
		t.Fatalf("checkQuery err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("expected failure on HTTP 500")
	}
}

func TestParseHTTPShapeKey(t *testing.T) {
	if _, p, err := parseHTTPShapeKey("GET /api/v0/repositories"); err != nil || p != "/api/v0/repositories" {
		t.Errorf("GET parse = %q, %v", p, err)
	}
	if method, p, err := parseHTTPShapeKey("POST /api/v0/code/dead-code"); err != nil || method != http.MethodPost || p != "/api/v0/code/dead-code" {
		t.Errorf("POST parse = method %q path %q err %v", method, p, err)
	}
	if _, _, err := parseHTTPShapeKey("bogus"); err == nil {
		t.Error("malformed key must be rejected")
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
