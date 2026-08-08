// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"reflect"
	"testing"
)

// A Kustomize `resources:` entry pointing at another repository must end up in
// resource_refs, where the catalog matcher can turn it into a cross-repo
// DEPLOYS_FROM edge. Only a path inside THIS repo belongs in bases.
//
// The classifier used to decide this by looking for "://", so it saw every
// scheme-less remote form as a local directory. Kustomize's own remote-target
// documentation lists those forms as supported: "github.com/Liujingfang1/mysql",
// "github.com/Liujingfang1/mysql?ref=test", and the SSH shorthand
// "git@github.com:owner/repo//someSubdir". None of them carry a scheme.
//
// Left uncaught, a remote base lands in KustomizeOverlay.bases — the graph then
// says another repository is a directory in this one — and drops out of
// resource_refs, so the typed DEPLOYS_FROM path never sees it.
func TestIsRemoteKustomizeRefRecognizesSchemelessForms(t *testing.T) {
	remote := []string{
		"https://github.com/kubernetes-sigs/kustomize//examples/multibases/dev/?timeout=120&ref=v3.3.1",
		"ssh://git@github.com/owner/repo//someSubdir",
		"file:///path/to/repo//someSubdir?ref=v3.3.1",
		"git@github.com:owner/repo//someSubdir",
		"git::https://example.com/repo.git",
		"github.com/acme/deployable-source//k8s?ref=v1.4.0",
		"github.com/Liujingfang1/mysql",
		"github.com/Liujingfang1/mysql?ref=test",
		"gitlab.com/acme/platform//overlays/prod",
		"bitbucket.org/acme/platform",
		"git.internal.example/team/repo//base",
	}
	for _, value := range remote {
		if !isRemoteKustomizeRef(value) {
			t.Errorf("isRemoteKustomizeRef(%q) = false, want true: kustomize resolves this to another repository", value)
		}
	}

	local := []string{
		"../base",
		"./base",
		"base",
		"overlays/prod",
		"../../deployment-helm/charts/service-edge-api",
		"/absolute/in/repo",
		// A single dotted segment names a directory, not a host. Only a dotted
		// first segment FOLLOWED by more path is host-shaped.
		"config.d",
		"v1.2.3",
	}
	for _, value := range local {
		if isRemoteKustomizeRef(value) {
			t.Errorf("isRemoteKustomizeRef(%q) = true, want false: this is a path inside the repo", value)
		}
	}
}

// The end-to-end shape of the same defect, driven through the real bucket
// builder: the fixture at tests/fixtures/ecosystems/kustomize-deployable-overlay
// carries one remote base and one local one, and its own comment says the
// remote entry "stays classified as a resource, not a base". It did not.
func TestParseKustomizationSplitsRemoteBaseFromLocalBase(t *testing.T) {
	document := map[string]any{
		"apiVersion": "kustomize.config.k8s.io/v1beta1",
		"kind":       "Kustomization",
		"resources": []any{
			"github.com/acme/deployable-source//k8s?ref=v1.4.0",
			"./base",
		},
	}

	overlay := parseKustomization(document, "kustomization.yaml", 1)

	wantBases := []string{"./base"}
	if got, ok := overlay["bases"].([]string); !ok || !reflect.DeepEqual(got, wantBases) {
		t.Errorf("bases = %#v, want %#v: only same-repo paths are bases", overlay["bases"], wantBases)
	}
	wantRefs := []string{"github.com/acme/deployable-source//k8s?ref=v1.4.0"}
	if got, ok := overlay["resource_refs"].([]string); !ok || !reflect.DeepEqual(got, wantRefs) {
		t.Errorf("resource_refs = %#v, want %#v: the remote repo must reach the catalog matcher",
			overlay["resource_refs"], wantRefs)
	}
}
